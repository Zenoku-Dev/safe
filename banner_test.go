package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestCFNoteInjected(t *testing.T) {
	page := `<!doctype html><html><head><title>Just a moment...</title></head><body><h1>Cloudflare check</h1></body></html>`
	base, _ := url.Parse("https://rutracker.org/forum/login.php")
	out := string(rewriteHTML(base, []byte(page), http.StatusForbidden))
	if !strings.Contains(out, "safe-note") {
		t.Fatalf("баннер не вставлен:\n%s", out)
	}
	out2 := string(rewriteHTML(base, []byte(page), http.StatusOK))
	if strings.Contains(out2, "safe-note") {
		t.Fatalf("баннер не должен вставляться на 200")
	}
	t.Log("ok")
}
