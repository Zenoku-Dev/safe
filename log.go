package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Журнал доступа: один JSON на строку, файл access-YYYY-MM-DD.log в SAFE_LOG_DIR.
// Храним 3 дня (ротация по дате + чистка при записи/старте).
// Логируем: время, IP клиента (последний хоп X-Forwarded-For, который дописывает rival-auth),
// гео (DB-IP Lite, CC BY 4.0), URL, метод, статус, байты, UA, Referer, длительность.
// Тело запроса/ответов НЕ логируется.

type accessEntry struct {
	TS         string `json:"ts"`
	IP         string `json:"ip"`
	Geo        string `json:"geo,omitempty"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Target     string `json:"target,omitempty"`
	Status     int    `json:"status"`
	Bytes      int    `json:"bytes"`
	DurationMS int64  `json:"dur_ms"`
	Proto      string `json:"proto"`
	UA         string `json:"ua,omitempty"`
	Referer    string `json:"ref,omitempty"`
}

var (
	logDir     = getenvDefault("SAFE_LOG_DIR", defaultLogDir())
	accessKeep = 3 * 24 * time.Hour

	logMu   sync.Mutex
	logFile *os.File
	logDate string

	geoOnce sync.Once
	geoIdx  *geoIndex
)

type geoIndex struct {
	m     map[[17]byte]uint8
	names []string
}

func getenvDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func defaultLogDir() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return "."
	}
	return filepath.Join(h, "safe")
}

// clientIP — публичный IP клиента: последний хоп XFF (его дописывает rival-auth),
// иначе RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

func loadGeo() {
	p := getenvDefault("SAFE_GEOIP", filepath.Join(logDir, "geoip.bin"))
	data, err := os.ReadFile(p)
	if err != nil {
		log.Printf("geoip: %v (гео будет пустым)", err)
		return
	}
	idx := &geoIndex{m: make(map[[17]byte]uint8, 1400000)}
	if len(data) < 16 || string(data[:8]) != "SAFEGEO1" {
		log.Printf("geoip: неверный формат %s", p)
		return
	}
	nrec := le32(data[8:])
	ntbl := le32(data[12:])
	pos := 16
	for i := uint32(0); i < ntbl; i++ {
		if pos >= len(data) {
			log.Printf("geoip: обрезанный файл %s", p)
			return
		}
		l := int(data[pos])
		pos++
		if pos+l > len(data) {
			return
		}
		idx.names = append(idx.names, string(data[pos:pos+l]))
		pos += l
	}
	for i := uint32(0); i < nrec; i++ {
		if pos+18 > len(data) {
			log.Printf("geoip: обрезанный файл %s", p)
			return
		}
		var k [17]byte
		copy(k[:16], data[pos:pos+16])
		k[16] = data[pos+16]
		idx.m[k] = data[pos+17]
		pos += 18
	}
	geoIdx = idx
	log.Printf("geoip: %d префиксов, %d кодов из %s", nrec, len(idx.names), p)
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func block16(ip [16]byte, nbits int) [17]byte {
	var out [17]byte
	copy(out[:16], ip[:])
	full, rem := nbits/8, uint(nbits%8)
	if rem != 0 {
		out[full] &= byte(0xff << (8 - rem))
		full++
	}
	for i := full; i < 16; i++ {
		out[i] = 0
	}
	out[16] = byte(nbits)
	return out
}

func geoCountry(ip string) string {
	geoOnce.Do(loadGeo)
	if geoIdx == nil {
		return ""
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	addr = addr.Unmap()
	var b [16]byte
	if addr.Is4() {
		a4 := addr.As4()
		copy(b[12:], a4[:])
	} else {
		b = addr.As16()
	}
	for l := 128; l >= 0; l-- {
		if i, ok := geoIdx.m[block16(b, l)]; ok {
			name := geoIdx.names[i]
			if name == "" {
				return ""
			}
			return name
		}
	}
	return ""
}

func rotateLocked() error {
	today := time.Now().Format("2006-01-02")
	if logFile != nil && today == logDate {
		return nil
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(logDir, "access-"+today+".log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	logFile, logDate = f, today
	// чистим старше 3 дней
	cutoff := time.Now().Add(-accessKeep).Format("2006-01-02")
	entries, err := os.ReadDir(logDir)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "access-") || !strings.HasSuffix(name, ".log") {
				continue
			}
			d := strings.TrimSuffix(strings.TrimPrefix(name, "access-"), ".log")
			if len(d) != len("2006-01-02") {
				continue
			}
			if d < cutoff {
				_ = os.Remove(filepath.Join(logDir, name))
				log.Printf("access-log: удалён старый %s", name)
			}
		}
	}
	return nil
}

func logAccess(e accessEntry) {
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	if err := rotateLocked(); err != nil {
		log.Printf("access-log: %v", err)
		return
	}
	_, _ = logFile.Write(append(line, '\n'))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}
func (sr *statusRecorder) Write(b []byte) (int, error) {
	n, err := sr.ResponseWriter.Write(b)
	sr.bytes += n
	return n, err
}

func logRequest(sr *statusRecorder, r *http.Request, start time.Time) {
	e := accessEntry{
		TS:         start.UTC().Format(time.RFC3339),
		Method:     r.Method,
		Path:       r.URL.RequestURI(),
		Status:     sr.status,
		Bytes:      sr.bytes,
		DurationMS: time.Since(start).Milliseconds(),
		Proto:      r.Proto,
		UA:         r.Header.Get("User-Agent"),
		Referer:    r.Header.Get("Referer"),
	}
	e.IP = clientIP(r)
	e.Geo = geoCountry(e.IP)
	if strings.HasPrefix(r.URL.Path, "/go/") {
		if t, err := parseTarget(strings.TrimPrefix(r.URL.Path, "/go/")); err == nil {
			e.Target = t.Host
		}
	}
	logAccess(e)
}
