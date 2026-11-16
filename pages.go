package main

import (
	"fmt"
	"html"
	"net/http"
)

// Общий стиль zenoku-design: чистый чёрный, белый глиф («logo-dark»),
// одна дуга с разрывом в правом верхнем углу, один штрих. Без акцентов, кроме глифа.

const markSVG = `<svg class="mark" viewBox="0 0 100 100" fill="none" aria-hidden="true"><path d="M78 40.4A29.6 29.6 0 1 1 60.1 22.2" stroke="currentColor" stroke-width="5.1" stroke-linecap="round"/><rect x="36.5" y="50" width="27" height="17.5" rx="4.5" stroke="currentColor" stroke-width="4.4"/><path d="M43.5 50V42.8a6.5 6.5 0 0 1 13 0V50" stroke="currentColor" stroke-width="4.4" stroke-linecap="round"/><circle cx="50" cy="58.4" r="2.1" fill="currentColor"/></svg>`

const faviconSVG = `data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'%3E%3Crect width='100' height='100' fill='%230a0a0a'/%3E%3Cpath d='M78 40.4A29.6 29.6 0 1 1 60.1 22.2' stroke='%23f2f2f0' stroke-width='6' fill='none' stroke-linecap='round'/%3E%3Crect x='36.5' y='50' width='27' height='18' rx='4.5' stroke='%23f2f2f0' stroke-width='5'/%3E%3Cpath d='M43.5 50V43a6.5 6.5 0 0 1 13 0V50' stroke='%23f2f2f0' stroke-width='5' fill='none'/%3E%3Ccircle cx='50' cy='58.5' r='2.4' fill='%23f2f2f0'/%3E%3C/svg%3E`

const pageHead = `<!doctype html><html lang="ru"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta name="theme-color" content="#0a0a0a">
<link rel="icon" href="` + faviconSVG + `">
<title>SAFE — сайт на http, открыт по https</title>
<style>
:root{--bg:#0a0a0a;--fg:#f2f2f0;--dim:#9a9a96;--line:#232326;--card:#131315}
*{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--fg);min-height:100vh;display:flex;flex-direction:column;
  font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Inter,Roboto,"Helvetica Neue",Arial,sans-serif;
  -webkit-font-smoothing:antialiased}
a{color:inherit}
header{display:flex;align-items:center;justify-content:space-between;padding:20px 26px}
.lockup{display:flex;align-items:center;gap:12px;text-decoration:none}
.lockup .mark{width:30px;height:30px;color:var(--fg)}
.lockup b{font-size:19px;font-weight:800;letter-spacing:.06em}
.headlink{color:var(--dim);text-decoration:none;font-size:13px;letter-spacing:.04em}
.headlink:hover{color:var(--fg)}
main{flex:1;display:flex;align-items:center;padding:24px 26px}
.wrap{width:100%;max-width:660px;margin:0 auto}
h1{font-size:clamp(46px,9.2vw,84px);line-height:.99;font-weight:850;letter-spacing:-.015em;
  text-transform:uppercase;margin-bottom:26px}
.sub{color:var(--dim);font-size:16.5px;line-height:1.55;max-width:52ch;margin-bottom:30px}
form{display:flex;gap:10px;margin-bottom:14px}
input[type=text]{flex:1;min-width:0;height:58px;background:var(--card);border:1px solid var(--line);
  border-radius:14px;padding:0 18px;color:var(--fg);
  font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:15px;outline:none}
input[type=text]:focus{border-color:var(--fg)}
input[type=text]::placeholder{color:#55534d}
button{height:58px;background:var(--fg);color:var(--bg);border:0;border-radius:14px;
  padding:0 28px;font:inherit;font-size:16px;font-weight:750;cursor:pointer;white-space:nowrap;
  transition:background .15s,color .15s}
button:hover{background:var(--bg);color:var(--fg);box-shadow:inset 0 0 0 1px var(--fg)}
.err{display:none;color:var(--fg);background:#17171b;border:1px solid var(--line);border-radius:12px;
  padding:12px 14px;font-size:13.5px;margin-bottom:10px}
.note{margin-top:26px;color:var(--dim);font-size:12.5px;line-height:1.6;border-top:1px solid var(--line);padding-top:16px}
.note code{background:var(--card);border:1px solid var(--line);border-radius:5px;padding:0 5px;font-size:11px}
footer{border-top:1px solid var(--line);padding:16px 26px;display:flex;align-items:center;
  justify-content:space-between;gap:14px;color:var(--dim);font-size:12px}
footer a{color:var(--dim);text-decoration:none;letter-spacing:.04em}
footer a:hover{color:var(--fg)}
footer .fmark{display:flex;align-items:center;gap:8px}
footer .fmark .mark{width:16px;height:16px;color:var(--dim)}
@media(max-width:560px){form{flex-direction:column}button{width:100%}h1{margin-bottom:20px}}
</style></head><body>
<header>
  <a class="lockup" href="/">` + markSVG + `<b>SAFE</b></a>
  <a class="headlink" href="https://github.com/Zenoku-Dev/safe">github →</a>
</header>`

const pageFoot = `<footer>
  <span class="fmark">` + markSVG + ` safe.rival-afk.com</span>
  <span>rival-afk.com</span>
</footer></body></html>`

const landingPage = pageHead + `<main><div class="wrap">
<h1>Сайт<br>на http.<br>Открыт<br>по https.</h1>
<p class="sub">Вставьте ссылку — http или https. Дальше сайт откроется внутри защищённого соединения на этом домене.</p>
<form id="f">
  <input id="u" type="text" inputmode="url" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="http://example.com" aria-label="Ссылка" autofocus>
  <button type="submit">Открыть</button>
</form>
<p class="err" id="e"></p>
<p class="note">Проксируются <code>http://</code> и <code>https://</code> (80/443) с публичных доменов. Редиректы ходим сервером, куки целей живут на этом домене — не вводите здесь пароли от важных аккаунтов. WebSocket не проксируется.</p>
</div></main>
<script>
var f=document.getElementById('f'),u=document.getElementById('u'),e=document.getElementById('e');
f.addEventListener('submit',function(ev){
  ev.preventDefault();
  var v=u.value.trim().replace(/\s+/g,'');
  e.style.display='none';
  if(!v)return;
  if(/^(ftp|news|mailto):\/\//i.test(v)){e.textContent='Проксируются только http:// и https:// ссылки.';e.style.display='block';return;}
  if(!/^https?:\/\//i.test(v))v='http://'+v;
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
<h1>Ещё<br>не то.</h1>
<p class="sub"><b style="color:var(--fg);font-weight:750">`+html.EscapeString(title)+`.</b><br>`+html.EscapeString(detail)+`</p>
<p class="note"><a href="/" style="color:var(--fg);text-decoration:none">← На главную</a></p>
</div></main>`+pageFoot)
}
