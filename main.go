// safe — HTTPS-обёртка для http-сайтов.
// На главной вставляешь http:// ссылку — дальше пользуешься сайтом как обычно,
// но снаружи всё ходит через https://safe.rival-afk.com.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

var (
	addr        = getenv("SAFE_ADDR", "127.0.0.1:8001")
	proxyURL    = getenv("SAFE_PROXY", "") // напр. socks5h://127.0.0.1:7780 (DNS на стороне прокси)
	maxBody     = int64(24 << 20)          // 24 MiB — лимит на ответ, который переписываем
	maxReqBody  = int64(24 << 20)
	httpTimeout = 30 * time.Second

	hostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
)

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// ---------- валидация цели ----------

var errNotHTTP = errors.New("можно проксировать только http:// ссылки")

// parseTarget разбирает строку после /go/ и возвращает нормализованный http-URL.
// Разрешены только http-цели по порту 80 с «человеческим» DNS-именем
// (никаких IP-литералов и разных числовых представлений — защита от SSRF в
// 127.x / 10.x / metadata).
func parseTarget(raw string) (*url.URL, error) {
	if len(raw) > 8192 {
		return nil, errors.New("слишком длинный URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" {
		return nil, errNotHTTP
	}
	if u.User != nil {
		return nil, errors.New("ссылки с логином/паролем не принимаются")
	}
	host := u.Hostname()
	port := u.Port()
	if port != "" && port != "80" {
		return nil, errors.New("разрешён только порт 80")
	}
	host = strings.ToLower(host)
	if host == "localhost" || !hostRe.MatchString(host) {
		return nil, errors.New("недопустимое имя хоста (нужны обычные публичные домены)")
	}
	u.Host = host // без :80
	if u.Path == "" {
		u.Path = "/"
	}
	return u, nil
}

// ---------- клиент для исходящих запросов ----------

func buildClient() *http.Client {
	tr := &http.Transport{
		DisableCompression: true,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConns:        64,
		IdleConnTimeout:     90 * time.Second,
	}
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			log.Fatalf("SAFE_PROXY: %v", err)
		}
		d, err := proxy.FromURL(u, proxy.Direct)
		if err != nil {
			log.Fatalf("SAFE_PROXY: %v", err)
		}
		cd, ok := d.(proxy.ContextDialer)
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if ok {
				return cd.DialContext(ctx, network, addr)
			}
			return d.Dial(network, addr)
		}
	} else {
		tr.Proxy = http.ProxyFromEnvironment
	}
	return &http.Client{
		Transport: tr,
		Timeout:   httpTimeout,
		// Редиректы не идём сами — переписываем Location и отдаём браузеру.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

var upstream = buildClient()

// ---------- заголовки ----------

var dropReqHeaders = map[string]bool{
	"Host": true, "Connection": true, "Content-Length": true, "Accept-Encoding": true,
	"Proxy-Connection": true, "Proxy-Authorization": true, "Upgrade": true,
	"Forwarded": true, "X-Forwarded-For": true, "X-Forwarded-Host": true,
	"X-Forwarded-Proto": true, "X-Real-Ip": true, "Cf-Connecting-Ip": true, "Te": true,
}
var dropRespHeaders = map[string]bool{
	"Connection": true, "Transfer-Encoding": true, "Content-Length": true,
	"Content-Security-Policy": true, "Content-Security-Policy-Report-Only": true,
	"X-Frame-Options": true, "Strict-Transport-Security": true, "X-Xss-Protection": true,
	"Cross-Origin-Opener-Policy": true, "Cross-Origin-Embedder-Policy": true,
	"Cross-Origin-Resource-Policy": true, "Permissions-Policy": true, "Referrer-Policy": true,
}

func copyReqHeaders(dst, src http.Header) {
	for k, vs := range src {
		if dropReqHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// rewriteSetCookies: срезаем Domain/Path у cookies цели, отдаём path=/ —
// куки запоминаются браузером на safe.rival-afk.com.
func rewriteSetCookies(h http.Header) {
	vs := h["Set-Cookie"]
	if len(vs) == 0 {
		return
	}
	out := make([]string, 0, len(vs))
	for _, vs1 := range vs {
		parts := strings.Split(vs1, ";")
		keep := parts[:0]
		for _, p := range parts {
			t := strings.TrimSpace(strings.ToLower(p))
			if strings.HasPrefix(t, "domain=") || strings.HasPrefix(t, "path=") {
				continue
			}
			keep = append(keep, p)
		}
		keep = append(keep, " Path=/")
		out = append(out, strings.Join(keep, ";"))
	}
	h["Set-Cookie"] = out
}

// ---------- основной обработчик /go/ ----------

func handleProxy(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimPrefix(r.URL.Path, "/go/")
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	up, err := parseTarget(target)
	if err != nil {
		writeError(w, http.StatusForbidden, "Сюда", err.Error())
		return
	}

	var body io.ReadCloser
	if r.Body != nil {
		body = http.MaxBytesReader(w, r.Body, maxReqBody)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, up.String(), body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Ошибка", err.Error())
		return
	}
	copyReqHeaders(req.Header, r.Header)
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := upstream.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Сайт не отвечает",
			fmt.Sprintf("%s: %v", up.Host, err))
		return
	}
	defer resp.Body.Close()

	// тело целиком (лимит) — нужно для переписывания html/css
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		writeError(w, http.StatusBadGateway, "Ошибка чтения", err.Error())
		return
	}
	if int64(len(raw)) > maxBody {
		writeError(w, http.StatusBadGateway, "Слишком большой ответ",
			"ответ больше 24 МБ — такое через обёртку не отдаём")
		return
	}

	ctype := resp.Header.Get("Content-Type")
	enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	if enc == "gzip" {
		if dec, derr := gunzip(raw); derr == nil {
			raw = dec
			enc = ""
			resp.Header.Del("Content-Encoding")
		}
	}

	base := resp.Request.URL // финальный URL (редиректы мы не дёргаем, но на всякий)
	if base == nil {
		base = up
	}

	isHTML := strings.Contains(strings.ToLower(ctype), "text/html")
	isCSS := strings.Contains(strings.ToLower(ctype), "text/css") ||
		strings.HasSuffix(strings.ToLower(up.Path), ".css")

	switch {
	case isHTML:
		raw = rewriteHTML(base, raw)
	case isCSS:
		raw = []byte(rewriteCSS(base, string(raw)))
	case enc != "" && enc != "identity":
		// неизвестная схема сжатия у html-подобного контента не переписывается —
		// отдаём как есть (браузер разберётся)
	}

	// редиректы: Location → /go/<абсолютный>
	if loc := resp.Header.Get("Location"); loc != "" {
		if ref, perr := url.Parse(loc); perr == nil {
			abs := base.ResolveReference(ref)
			if abs.Scheme != "http" {
				writeError(w, http.StatusForbidden, "Редирект на https",
					fmt.Sprintf("сайт редиректит на %s — такие сайты уже на https, открывайте напрямую", abs))
				return
			}
			resp.Header.Set("Location", "/go/"+abs.String())
		}
	}

	for k := range resp.Header {
		if dropRespHeaders[http.CanonicalHeaderKey(k)] {
			resp.Header.Del(k)
		}
	}
	rewriteSetCookies(resp.Header)

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(raw)))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(raw)
}

func main() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/healthz":
			w.Write([]byte("ok\n"))
		case p == "/go":
			http.Redirect(w, r, "/", http.StatusFound)
		case strings.HasPrefix(p, "/go/"):
			handleProxy(w, r)
		case p == "/":
			serveLanding(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	log.Printf("safe: слушаю %s (proxy: %q)", addr, proxyURL)
	log.Fatal(srv.ListenAndServe())
}
