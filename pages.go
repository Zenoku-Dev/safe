package main

import (
	"fmt"
	"html"
	"net/http"
)

const pageHead = `<!doctype html><html lang="ru"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>safe — http-сайты по https</title>
<style>
:root{--bg:#101012;--fg:#ece9e2;--dim:#8f8c85;--acc:#e8b64c;--line:#26262a;--card:#16161a}
*{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--fg);min-height:100vh;display:flex;flex-direction:column;
  font:16px/1.55 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif}
main{flex:1;display:flex;align-items:center;justify-content:center;padding:40px 20px}
.wrap{width:100%;max-width:560px}
.mark{display:flex;align-items:center;gap:10px;color:var(--dim);font-size:13px;margin-bottom:44px}
.mark svg{display:block}
h1{font-size:34px;line-height:1.18;font-weight:650;letter-spacing:-.02em;margin-bottom:14px}
h1 .a{color:var(--acc)}
.sub{color:var(--dim);margin-bottom:28px;max-width:46ch}
form{display:flex;gap:10px}
input[type=text]{flex:1;min-width:0;background:var(--card);border:1px solid var(--line);border-radius:12px;
  padding:14px 16px;color:var(--fg);font:inherit;outline:none}
input[type=text]:focus{border-color:var(--acc)}
input[type=text]::placeholder{color:#5a5852}
button{background:var(--acc);color:#1a150a;border:0;border-radius:12px;padding:0 20px;
  font:inherit;font-weight:650;cursor:pointer;white-space:nowrap}
button:hover{filter:brightness(1.06)}
.err{margin-top:14px;color:#ff8d6b;font-size:14px;display:none}
.note{margin-top:28px;color:var(--dim);font-size:12.5px;border-top:1px solid var(--line);padding-top:16px}
.note code{background:var(--card);border:1px solid var(--line);border-radius:5px;padding:1px 5px;font-size:11px}
footer{padding:18px 20px;border-top:1px solid var(--line);color:#5a5852;font-size:12px;
  display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap}
footer a{color:var(--dim);text-decoration:none}
footer a:hover{color:var(--fg)}
@media(max-width:520px){form{flex-direction:column}button{padding:14px}}
</style></head><body>`

const pageFoot = `<footer><span>safe.rival-afk.com</span><span><a href="https://zenoku.host">zenoku</a></span></footer></body></html>`

const landingPage = pageHead + `<main><div class="wrap">
<div class="mark"><svg width="22" height="22" viewBox="0 0 100 100" fill="none" aria-hidden="true"><circle cx="50" cy="50" r="42" stroke="#e8b64c" stroke-width="9"/><circle cx="50" cy="50" r="10" fill="#ece9e2"/></svg>safe · rival-afk</div>
<h1>Сайт на http.<br>Открыт по <span class="a">https</span>.</h1>
<p class="sub">Вставьте ссылку на сайт, который работает только по обычному http, — дальше он будет открываться внутри защищённого соединения.</p>
<form id="f">
  <input id="u" type="text" inputmode="url" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="http://example.com" aria-label="Ссылка http://" autofocus>
  <button type="submit">Открыть</button>
</form>
<p class="err" id="e"></p>
<p class="note">Оборачиваются только адреса <code>http://</code> (80 порт). https-сайты открывайте напрямую. Куки целей сохраняются на этом домене, не вводите здесь пароли от важных аккаунтов.</p>
</div></main>
<script>
var f=document.getElementById('f'),u=document.getElementById('u'),e=document.getElementById('e');
f.addEventListener('submit',function(ev){
  ev.preventDefault();
  var v=u.value.trim().replace(/\s+/g,'');
  e.style.display='none';
  if(!v)return;
  if(/^https:\/\//i.test(v)){e.textContent='Это уже https — такой сайт безопасен сам по себе, откройте его напрямую.';e.style.display='block';return;}
  if(!/^http:\/\//i.test(v))v='http://'+v;
  location.assign('/go/'+v);
});
</script>` + pageFoot

func serveLanding(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, landingPage)
}

func writeError(w http.ResponseWriter, code int, title, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprint(w, pageHead+`<main><div class="wrap">
<div class="mark"><svg width="22" height="22" viewBox="0 0 100 100" fill="none" aria-hidden="true"><circle cx="50" cy="50" r="42" stroke="#e8b64c" stroke-width="9"/><circle cx="50" cy="50" r="10" fill="#ece9e2"/></svg>safe · rival-afk</div>
<h1>`+html.EscapeString(title)+`</h1>
<p class="sub">`+html.EscapeString(detail)+`</p>
<p class="note"><a href="/" style="color:var(--acc);text-decoration:none">← На главную</a></p>
</div></main>`+pageFoot)
}
