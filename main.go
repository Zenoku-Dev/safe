// safe — HTTPS-обёртка для http-сайтов.
// На главной вставляешь http:// ссылку — дальше пользуешься сайтом как обычно,
// но снаружи всё ходит через https://safe.rival-afk.com.
package main

import (
	"context"
	"crypto/tls"
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

var errNotHTTP = errors.New("можно проксировать только http:// и https:// ссылки")

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
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errNotHTTP
	}
	if u.User != nil {
		return nil, errors.New("ссылки с логином/паролем не принимаются")
	}
	host := u.Hostname()
	port := u.Port()
	if u.Scheme == "http" && port != "" && port != "80" {
		return nil, errors.New("для http разрешён только порт 80")
	}
	if u.Scheme == "https" && port != "" && port != "443" {
		return nil, errors.New("для https разрешён только порт 443")
	}
	host = strings.ToLower(host)
	if host == "localhost" || !hostRe.MatchString(host) {
		return nil, errors.New("недопустимое имя хоста (нужны обычные публичные домены)")
	}
	u.Host = host // порты по умолчанию не пишем
	if u.Path == "" {
		u.Path = "/"
	}
	return u, nil
}

// ---------- клиент для исходящих запросов ----------

func buildClient() *http.Client {
	// Параметры TLS максимально близкие к Chrome (JA3): иначе CF Turnstile
	// считает клиента ботом и отклоняет его proof даже при корректном VM.
	chromeSuites := []uint16{
		tls.TLS_AES_128_GCM_SHA256, tls.TLS_CHACHA20_POLY1305_SHA256, tls.TLS_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
		tls.TLS_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_RSA_WITH_AES_128_CBC_SHA, tls.TLS_RSA_WITH_AES_256_CBC_SHA,
	}
	tr := &http.Transport{
		DisableCompression:    true,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConns:          64,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
		TLSClientConfig: &tls.Config{
			MinVersion:       tls.VersionTLS12,
			CipherSuites:     chromeSuites,
			CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384},
		},
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


// targetBaseFromCookie — та же операция, но целевая схема+хост берутся из cookie
// safe_target (её выставляет прокси на каждом /go/-ответе).
// Нужна, когда страница уже «ушла» на корневой путь (CF challenge заменяет URL
// на /forum/...) и Referer больше не указывает на /go/.
func targetBaseFromCookie(r *http.Request) (string, bool) {
	for _, c := range r.Cookies() {
		if c.Name != "safe_target" {
			continue
		}
		v, err := url.QueryUnescape(c.Value)
		if err != nil {
			return "", false
		}
		t, err := parseTarget(v + "/")
		if err != nil {
			return "", false
		}
		return t.Scheme + "://" + t.Host, true
	}
	return "", false
}

// targetBaseFromReferer извлекает "scheme://host" цели из Referer вида
// /go/<http|https://host>[:port]/путь. Строго через parseTarget — тот же
// вайтлист публичных хостов и портов.
func targetBaseFromReferer(ref string) (string, bool) {
	if ref == "" {
		return "", false
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", false
	}
	p := u.Path
	if !strings.HasPrefix(p, "/go/") {
		return "", false
	}
	rest := strings.TrimPrefix(p, "/go/")
	t, err := parseTarget(rest)
	if err != nil {
		return "", false
	}
	return t.Scheme + "://" + t.Host, true
}

func handleProxy(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimPrefix(r.URL.Path, "/go/")
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	proxyTo(w, r, target)
}

func proxyTo(w http.ResponseWriter, r *http.Request, target string) {
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
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	}
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
			if abs.Scheme != "http" && abs.Scheme != "https" {
				writeError(w, http.StatusForbidden, "Редирект на другое",
					fmt.Sprintf("сайт редиректит на %s — поддерживаются только http/https", abs))
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
	// cookie safe_target — схема+хост текущей цели для catch-all fallback'а
	// (корневые-относительные пути, когда Referer уже ушёл с /go/).
	if getenvDefault("SAFE_TARGET_COOKIE", "1") != "0" {
		resp.Header["Set-Cookie"] = append(resp.Header["Set-Cookie"],
			"safe_target="+url.QueryEscape(up.Scheme+"://"+up.Host)+"; Path=/; HttpOnly; Secure; SameSite=Lax")
	}

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
		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer logRequest(sr, r, start)
		w = sr
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
			// Корневые-относительные пути из JS/inline-скриптов страниц,
			// отданных через /go/<цель> (например /cdn-cgi/challenge-platform… у Cloudflare).
			// Сначала смотрим Referer (/go/<цель>/…), затем — cookie safe_target.
			// Отдаём контент ЦЕЛИ по тому же пути БЕЗ редиректа — страница-инициатор
			// ждёт скрипт ровно по /cdn-cgi/… на своём origin.
			base, ok := targetBaseFromReferer(r.Header.Get("Referer"))
			if !ok {
				base, ok = targetBaseFromCookie(r)
			}
			if ok {
				q := ""
				if r.URL.RawQuery != "" {
					q = "?" + r.URL.RawQuery
				}
				proxyTo(w, r, base+p+q)
				return
			}
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
