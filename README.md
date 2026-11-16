# safe — HTTP → HTTPS обёртка

`https://safe.rival-afk.com` — вставляешь ссылку `http://…` или `https://…`
на главной странице, дальше пользуешься сайтом «как обычно» в защищённом соединении.

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
                     │  исходящие по умолчанию — напрямую с Wi‑Fi redmi (`SAFE_EXIT=direct`),
                     │  DNS резолвится через DoH (8.8.8.8/1.1.1.1), TLS/сертификаты
                     │  проверяются системным CA (`SSL_CERT_FILE=.../etc/tls/cert.pem`)
                     ▼
   целевой сайт (http :80 / https :443)
```

- Редиректы внутри http/https переписываются в `/go/<цель>` и отдаются браузеру.
- HTML/CSS переписываются: `href/src/action/srcset/url()` → `/go/<абсолютный URL>`,
  `<base>` и JS-шим для XHR/fetch/setAttribute.
- Cookies цели срезаются по `Domain`/`Path` и живут на safe.rival-afk.com.
- `http://` (порт 80) и `https://` (порт 443), публичные DNS-имена
  (IP-литералы не принимаются — см. SSRF-замечание ниже).
  Самоподписанные сертификаты целей не принимаются (TLS проверяется обычным CA).

### deps/ (uTLS, не коммитится)

`deps/` — вендоринг Go-библиотек uTLS из Debian-пакетов (`./scripts/prepare-deps.sh`).
В `go.mod` активны `replace`-директивы. Скрипт идемпотентен (перекачивает пакеты заново).

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

### Журнал доступа (2026-11)

Каждое подключение логируется одной JSON-строкой в `~/safe/access-YYYY-MM-DD.log`
на redmi (chmod 600, каталог `SAFE_LOG_DIR`, по умолчанию `~/safe`):

```json
{"ts":"2026-11-16T07:12:00Z","ip":"203.0.113.7","geo":"SE","method":"GET",
 "path":"/go/http://example.com/","target":"example.com","status":200,
 "bytes":1523,"dur_ms":812,"proto":"HTTP/2.0","ua":"Mozilla/5.0 ...","ref":"..."}
```

- IP — последний хоп `X-Forwarded-For` (его дописывает rival-auth при проксировании;
  первые хопы могут быть подделаны клиентом).
- Geo — локальная база DB-IP Lite "Country" (CC BY 4.0), `geoip.bin` (~24 МБ,
  собирается из `dbip-country.mmdb` через `sapics/ip-location-db` любым
  MMDB-парсером; в репо не коммитится).
- **Хранение 3 дня**: файл на дату; старше трёх дней удаляются при записи.
- Тела запросов/ответов не логируются.
- На странице — уведомление о логировании (в блоке note).

## Проверка

```sh
rsh phone 'curl -s http://127.0.0.1:8001/healthz'     # ok
rsh phone 'ls -la ~/safe/access-*.log && tail -3 ~/safe/access-*.log'   # журнал
rsh vps  'curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:18003/'
curl -s -o /dev/null -w "%{http_code}\n" https://safe.rival-afk.com/
```

## Ограничения и безопасность

- Это **открытый** прокси: любой, кто знает домен, может ходить через него
  на http/https-сайты (решение сознательное).
- Исходящий трафик выходит с IP VPS через vps-side socks; цель с приватным
  DNS-именем (DNS-rebind) теоретически дотягивается до `127.0.0.1`/внутренних
  хостов подсети VPS на порту 80. Всё прочее (IP-литералы, не-80 порты,
  непубличные TLD) отклонено.
- Не логиньтесь под важными паролями: cookies и содержимое страниц видит сервер.
- Тяжёлое/стриминговое/WS-heavy — может быть медленно или ломаться
  (WebSocket не проксируется).
- Прокси выставляет cookie `safe_target=<scheme://host>` на каждом /go/-ответе:
  корневые-относительные пути в JS (`/cdn-cgi/...`, `/forum/...`) редиректятся
  в текущую цель (сначала Referer, потом cookie — 307, метод и тело сохраняются).
- User-Agent браузера пересылается как есть (иначе сайты с bot-detection
  отдают не контент, а страницу-заглушку). Интерактивные challenge'и
  (Cloudflare и т.п.) должны решаться в браузере пользователя: все запросы
  идут через один и тот же выходной IP VPS, cookie challenge'а сохраняются на
  safe.rival-afk.com. Гарантии, что конкретная CF-зона пропустит прокси, нет.

### Режим выхода (обновлено 2026-11-16)

По умолчанию (`SAFE_EXIT=direct`) safe выходит напрямую с redmi (Wi‑Fi), а не через
VPS-socks. Это важно для сайтов, которые Cloudflare проверяет по сетевому пути:
с резидентского Wi‑Fi rutracker.org открывается (проверено `200`, `111 216` байт),
с датацентрового IP VPS — managed-challenge.

DNS: системный резолвер redmi для uid u0_a165 сломан, поэтому используется DoH
`https://8.8.8.8/resolve` / `https://1.1.1.1/dns-query` (SNI: dns.google / cloudflare-dns.com).
TLS: нужен корректный корневой CA-бандл — в runit `run` выставлен `SSL_CERT_FILE=
/data/data/com.termux/files/usr/etc/tls/cert.pem`.

Вернуться на VPS-выход: `SAFE_EXIT=socks` + `SAFE_PROXY=socks5h://127.0.0.1:7780`.

Сервис runit: `$PREFIX/var/service/safe/run` — обязательно `SSL_CERT_FILE=...`
(иначе Go отклоняет сертификаты на redmi).
