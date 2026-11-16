# safe — HTTP → HTTPS обёртка

`https://safe.rival-afk.com` — вставляешь ссылку `http://…` на главной странице,
дальше пользуешься сайтом «как обычно», но снаружи всё идёт по https.

## Как устроено

```
браузер ──https──> safe.rival-afk.com (Cloudflare/DNS → 46.183.21.121)
                     │
                     ▼
   rival-auth :443 (ACME-TLS, /etc/rival/auth.json proxy_hosts)
                     │
                     ▼  proxy_pass http://127.0.0.1:18003
   redmi: reverse tunnel (-R 18003 → redmi:8001)
                     │
                     ▼
   redmi :127.0.0.1:8001 — это приложение (runit `safe`, бинарник ~/safe/safe)
                     │  исходящие — через SSH-socks redmi (~/.rsh/tunnel.sh, -D 7780),
                     │  DNS резолвится на стороне VPS (socks5h)
                     ▼
   целевой http-сайт (только :80)
```

- Редиректы `http→http` переписываются в `/go/<цель>` и отдаются браузеру.
- Редиректы `http→https` — страница-заглушка «откройте напрямую».
- HTML/CSS переписываются: `href/src/action/srcset/url()` → `/go/<абсолютный URL>`,
  `<base>` и JS-шим для XHR/fetch/setAttribute.
- Cookies цели срезаются по `Domain`/`Path` и живут на safe.rival-afk.com.
- Строго `http://`, порт 80, публичные DNS-имена (IP-литералы не принимаются —
  см. SSRF-замечание ниже).

## Исходники → redmi

```sh
# сборка (dev, arm32 для redmi)
cd ~/projects/safe
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -o safe-linux-arm .

# деплой
rsh push phone safe-linux-arm ~/safe/safe
rsh phone 'chmod 755 ~/safe/safe && sv restart safe'
```

Сервис runit: `$PREFIX/var/service/safe/run` (`exec ~/safe/safe`,
`SAFE_ADDR=127.0.0.1:8001`, `SAFE_PROXY=socks5h://127.0.0.1:7780`),
логи — `svlogd ~/safe/log`.

Туннель: форвард `VPS:127.0.0.1:18003 → redmi:127.0.0.1:8001` в базовой сессии
`./zenoku/tunnel.sh` (канон) → `rsh push phone tunnel.sh ~/.rsh/tunnel.sh && rsh phone 'sv restart tunnel'`.

TLS: `proxy_hosts["safe.rival-afk.com"] = "http://127.0.0.1:18003"` в `/etc/rival/auth.json`
(бэкап `auth.json.bak-*`, рестарт `systemctl restart rival-auth`, сертификат ACME автоматически,
нужен A-запись в Cloudflare → 46.183.21.121, DNS-only).

## Проверка

```sh
rsh phone 'curl -s http://127.0.0.1:8001/healthz'     # ok
rsh vps  'curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:18003/'
curl -s -o /dev/null -w "%{http_code}\n" https://safe.rival-afk.com/
```

## Ограничения и безопасность

- Это **открытый** прокси: любой, кто знает домен, может ходить через него
  на http-сайты (решение сознательное).
- Исходящий трафик выходит с IP VPS через vps-side socks; цель с приватным
  DNS-именем (DNS-rebind) теоретически дотягивается до `127.0.0.1`/внутренних
  хостов подсети VPS на порту 80. Всё прочее (IP-литералы, не-80 порты,
  непубличные TLD) отклонено.
- Не логиньтесь под важными паролями: cookies и содержимое страниц видит сервер.
- Тяжёлое/стриминговое/WS-heavy — может быть медленно или ломаться
  (WebSocket не проксируется).
