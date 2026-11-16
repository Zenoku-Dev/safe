package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

func gunzip(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(r, maxBody+1))
}

// proxyPrefix — префикс, под которым живёт цель (абсолютный http-URL)
func proxyPrefix(u *url.URL) string {
	return "/go/" + strings.TrimSuffix(u.Scheme+"://"+u.Host, "/")
}

// rewriteOne переписывает одну ссылку относительно base.
func rewriteOne(base *url.URL, raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(s, "#") {
		return raw
	}
	low := strings.ToLower(s)
	for _, p := range []string{"javascript:", "mailto:", "tel:", "data:", "blob:", "about:", "chrome:", "geo:", "sms:"} {
		if strings.HasPrefix(low, p) {
			return raw
		}
	}
	if strings.HasPrefix(s, "/go/") { // уже переписано
		return s
	}
	ref, err := url.Parse(s)
	if err != nil {
		return raw
	}
	abs := base.ResolveReference(ref)
	switch abs.Scheme {
	case "http", "https":
		// Cloudflare-инфраструктура Turnstile работает напрямую из браузера
		// пользователя (настоящий TLS-фингерпринт) — через прокси CF её отклоняет.
		if isChallengeHost(abs.Hostname()) {
			return abs.String()
		}
		return "/go/" + abs.String()
	default:
		return raw
	}
}


// isChallengeHost — без регистрации для Cloudflare challenge/Turnstile (вызовы
// из браузера пользователя, а не через прокси).
func isChallengeHost(h string) bool {
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	return h == "challenges.cloudflare.com" || strings.HasSuffix(h, ".challenges.cloudflare.com")
}

var (
	cssURLRe    = regexp.MustCompile(`(?is)url\s*\(\s*([^)]*?)\s*\)`)
	cssImportRe = regexp.MustCompile(`(?is)@import\s+['"]([^'"]+)['"]`)
)

func rewriteCSS(base *url.URL, css string) string {
	css = cssURLRe.ReplaceAllStringFunc(css, func(m string) string {
		sub := cssURLRe.FindStringSubmatch(m)
		inner := strings.TrimSpace(sub[1])
		if len(inner) >= 2 {
			if q := inner[0]; (q == '\'' || q == '"') && inner[len(inner)-1] == q {
				inner = inner[1 : len(inner)-1]
			}
		}
		return "url('" + rewriteOne(base, inner) + "')"
	})
	css = cssImportRe.ReplaceAllStringFunc(css, func(m string) string {
		sub := cssImportRe.FindStringSubmatch(m)
		return `@import '` + rewriteOne(base, sub[1]) + `'`
	})
	return css
}

// атрибуты, которые переписываем: тег -> список атрибутов
var attrRules = map[string][]string{
	"a": {"href"}, "area": {"href"},
	"link":   {"href"},
	"script": {"src"},
	"img":    {"src", "srcset"}, "input": {"src"},
	"source": {"src", "srcset"}, "track": {"src"},
	"iframe": {"src"}, "frame": {"src"},
	"embed": {"src"}, "object": {"data"},
	"form":  {"action"},
	"video": {"src", "poster"}, "audio": {"src"},
	"blockquote": {"cite"}, "q": {"cite"}, "del": {"cite"}, "ins": {"cite"},
	"body": {"background"}, "table": {"background"}, "td": {"background"}, "th": {"background"},
}

const jsShim = `<script>(function(){function pb(){var m=location.pathname.match(/^(\/go\/https?:\/\/[^\/]+)/);return m?m[1]:'';}
function rw(u){if(!u)return u;u=String(u);var l=u.toLowerCase();if(l.indexOf('/go/')===0)return u;if(/^(javascript|mailto|tel|data|blob|#)/.test(l))return u;if(/^https?:\/\//.test(l))return '/go/'+u;if(u.charAt(0)==='/')return pb()+u;return u;}
var of=window.fetch;if(of)window.fetch=function(u,o){if(typeof u==='string'){u=rw(u);}else if(u&&u.url){u=new Request(rw(u.url),u);}return of.call(this,u,o);};
var oo=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(m,u){var a=Array.prototype.slice.call(arguments);if(a[1]!=null)a[1]=rw(a[1]);return oo.apply(this,a);};
var sa=Element.prototype.setAttribute;Element.prototype.setAttribute=function(n,v){if(/^(src|href|action|poster|data)$/i.test(n))v=rw(v);return sa.call(this,n,v);};})();</script>`

func htmlIsHead(n *html.Node) bool {
	return n.Type == html.ElementNode && strings.EqualFold(n.Data, "head")
}

// rewriteHTML разбирает документ, переписывает ссылки, выкидывает существующие
// <base>/<meta CSP>, вставляет свои <base> и JS-шим.
func rewriteHTML(base *url.URL, doc []byte) []byte {
	root, err := html.Parse(bytes.NewReader(doc))
	if err != nil {
		return doc
	}
	prefix := proxyPrefix(base)

		shimEnabled := getenvDefault("SAFE_SHIM", "1") != "0"
	baseEnabled := getenvDefault("SAFE_BASE", "1") != "0"

	// <base> href = префикс + «каталог» текущей страницы
	dir := base.Path
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		dir = dir[:i+1]
	} else {
		dir = "/"
	}
	baseHref := prefix + dir

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			// выкидываем существующие base / meta CSP
			if tag == "base" {
				n.Parent.RemoveChild(n)
				return
			}
			if tag == "meta" {
				httpEquiv := ""
				for _, a := range n.Attr {
					if strings.EqualFold(a.Key, "http-equiv") {
						httpEquiv = strings.ToLower(a.Val)
					}
				}
				if strings.HasPrefix(httpEquiv, "content-security-policy") {
					n.Parent.RemoveChild(n)
					return
				}
			}
			// meta refresh: content="3; url=http://..."
			if tag == "meta" {
				for i, a := range n.Attr {
					if !strings.EqualFold(a.Key, "content") {
						continue
					}
					m := regexp.MustCompile(`(?i)^(\s*\d+\s*;\s*url\s*=\s*)(\S+)(.*)$`).FindStringSubmatch(a.Val)
					if m != nil {
						n.Attr[i].Val = m[1] + rewriteOne(base, m[2]) + m[3]
					}
				}
			}
			// переписываем атрибуты
			rules := attrRules[tag]
			for i, a := range n.Attr {
				key := strings.ToLower(a.Key)
				for _, r := range rules {
					if key != r {
						continue
					}
					if r == "srcset" {
						n.Attr[i].Val = rewriteSrcset(base, a.Val)
					} else {
						n.Attr[i].Val = rewriteOne(base, a.Val)
					}
				}
				if key == "style" {
					n.Attr[i].Val = rewriteCSS(base, a.Val)
				}
			}
			// <style>…</style>
			if tag == "style" {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.TextNode {
						c.Data = rewriteCSS(base, c.Data)
					}
				}
			}
		}
		// обход детей — копируем список, т.к. удаляем узлы
		var children []*html.Node
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			children = append(children, c)
		}
		for _, c := range children {
			walk(c)
		}
	}
	walk(root)

	// вставляем <base> и шим в начало <head>
	var findHead func(*html.Node) *html.Node
	findHead = func(n *html.Node) *html.Node {
		if htmlIsHead(n) {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if h := findHead(c); h != nil {
				return h
			}
		}
		return nil
	}
	if head := findHead(root); head != nil {
		inject := func(fragment string) bool {
			nodes, err := html.ParseFragment(strings.NewReader(fragment), head)
			if err != nil {
				return false
			}
			for _, sn := range nodes {
				head.InsertBefore(sn, head.FirstChild)
			}
			return true
		}
		if shimEnabled {
			inject(jsShim)
		}
		if baseEnabled {
			inject(`<base href="` + baseHref + `">`)
		}
	}

	var buf bytes.Buffer
	if err := html.Render(&buf, root); err != nil {
		return doc
	}
	return buf.Bytes()
}

func rewriteSrcset(base *url.URL, srcset string) string {
	parts := strings.Split(srcset, ",")
	for i, p := range parts {
		fields := strings.Fields(p)
		if len(fields) == 0 {
			continue
		}
		fields[0] = rewriteOne(base, fields[0])
		parts[i] = strings.Join(fields, " ")
	}
	return strings.Join(parts, ", ")
}
