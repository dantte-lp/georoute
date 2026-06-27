# Изменения

Все значимые изменения этого проекта документируются здесь.
Формат — [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/),
версионирование — [SemVer](https://semver.org/lang/ru/spec/v2.0.0.html).

## [Unreleased]

[Unreleased]: https://github.com/dantte-lp/georoute/compare/v2.1.0...HEAD

## [2.1.0] - 2026-06-27

### Добавлено
- Daemon-режим через `--refresh-interval=N` (`GEOROUTE_REFRESH_INTERVAL`).
  В сочетании с `--http-addr` процесс гоняет пайплайн на внутреннем
  тикере вместо exit-после-цикла. SIGTERM отменяет цикл и дренит
  in-flight goroutine (nft / frr-reload не оставляется в полу-применённом
  состоянии).
- 8 новых операторских флагов с матчингом на `GEOROUTE_*` env-vars:
  - `--http-timeout` (по умолчанию `60s`)
  - `--frr-reload-timeout` (по умолчанию `3m`)
  - `--nft-timeout` (по умолчанию `30s`)
  - `--retry-attempts` (по умолчанию `3`)
  - `--retry-base-delay` (по умолчанию `2s`)
  - `--frr-reload-script` (по умолчанию `/usr/lib/frr/frr-reload.py`)
  - `--nft-binary` (по умолчанию `/usr/sbin/nft`)
  - `--refresh-interval` (по умолчанию `0` = oneshot-семантика)
- Новый Prometheus-counter `georoute_skipped_overlap_total{country}` —
  инкремент при пропуске тика, потому что предыдущий цикл ещё держит
  mutex. Алёртинг по rate этого counter — ранний сигнал роста latency.
- Per-cycle `run_id` в JSON-логах (daemon-режим). В oneshot — по-прежнему
  один id на процесс (поведение v2.0).

### Изменено
- `realMain` теперь синхронно биндит HTTP-listener через новый
  `healthServer.preBind()` ДО старта пайплайна. Раньше listener
  открывался в goroutine, и `EADDRINUSE` / `EACCES` падал в лог,
  но процесс продолжал парковаться — systemd видел unit UP при
  мёртвом `/metrics`. Теперь bind-ошибка = exit 1.
- `applyFRRConfigOpts` принимает путь к frr-reload-скрипту и таймаут
  через `applyOpts`, а не через package-константы.
- `fetch`/`fetchWithRetry`/`fetchWithCache` тянут HTTP-timeout,
  число ретраев и базовый delay из `cliFlags`.
- `applyNft` читает путь к nft и timeout из `cliFlags`.

### Исправлено
- Daemon-shutdown: `refreshLoop` дренит in-flight работу через
  `sync.WaitGroup` перед return'ом — SIGTERM строго упорядочен
  (ctx cancel → in-flight цикл завершается → HTTP server drains →
  exit).

### Эксплуатация
- Новый файл: `roles/georoute/defaults/main.yml` в polyexit-prod
  с toggle `georoute_mode: oneshot | daemon` + env-vars для новых
  флагов.
- dev-04 переключён в daemon-режим (`127.0.0.1:9494`, рефреш каждые
  12h, log-format=json). `9090` на dev-04 занят crowdsec
  ocserv-bouncer'ом — переехали на 9494.

[2.1.0]: https://github.com/dantte-lp/georoute/releases/tag/v2.1.0

## [2.0.0] - 2026-06-27

Umbrella-релиз для PR'ов, приземлившихся между v1.0 и этим tag'ом
(#6, #7, #8, #10, #11). Каждая новая фича opt-in — пустые значения
флагов сохраняют поведение v1 — поэтому существующие `.env` файлы
работают без правок.

### Добавлено — Operator extras (#6)
- `--extras-v4-file` / `--extras-v6-file` (`GEOROUTE_EXTRAS_V4_FILE`,
  `GEOROUTE_EXTRAS_V6_FILE`) мёрджат оператор-managed списки префиксов
  (например CDN, не входящие в RIPE-фид страны) с RIPE-ответом перед
  агрегацией. Parser строгий через `netip.ParsePrefix`; невалидные
  строки фейлят load с указанием номера строки.

### Добавлено — Кэш-fallback + FRR rollback (#7)
- `--cache-file` (`GEOROUTE_CACHE_FILE`, по умолчанию
  `/var/lib/georoute/feed-<cc>.json.gz`) — gzip+JSON снимок последнего
  успешного RIPE-ответа. На последовательных RIPE 5xx бинарь
  fallback'ится на кэш (если свежий) и эмитит
  `georoute_fetches_total{source="cache"}`.
- `--cache-max-age` (`GEOROUTE_CACHE_MAX_AGE`, по умолчанию 7 дней).
- FRR-rollback: `vtysh -C` pre-validate'ит staged-конфиг; при reload
  failure предыдущий known-good `frr.conf` восстанавливается побайтно,
  и `frr-reload.py` запускается снова для resync'а in-memory state'а
  FRR.

### Добавлено — Healthcheck HTTP server (#8)
- Опциональный `--http-addr=:port` (например `127.0.0.1:9090`)
  поднимает embedded HTTP-сервер:
  - `GET /live` — всегда 200; не трогает downstream'ы.
  - `GET /ready` — 200 если last-success свежее чем `--ready-max-age`
    (по умолчанию 24h), иначе 503.
  - `GET /debug/pprof/*` — Go runtime profiles.
- `--last-success-file` (`GEOROUTE_LAST_SUCCESS_FILE`, по умолчанию
  `/var/lib/georoute/last-success-<cc>`) — timestamp.
- `--ready-max-age` (`GEOROUTE_READY_MAX_AGE`, по умолчанию 24h).
- Когда `--http-addr` set, процесс паркуется на SIGTERM после
  oneshot-работы (в v2.1 добавлен daemon-вариант через
  `--refresh-interval`).

### Добавлено — Prometheus /metrics (#10)
- Эндпоинт `/metrics` на том же сервере, шарит один
  `*prometheus.Registry` между healthcheck-библиотекой, Go-runtime
  коллекторами и app metrics — один scrape покрывает всё.
- Новые серии:
  - `georoute_runs_total{country, result}`
  - `georoute_fetches_total{country, source, result}`
  - `georoute_nft_applies_total{country, result}`
  - `georoute_frr_reloads_total{country, result}`
  - `georoute_prefixes{country, family, source}` (ripe / extras / merged)
  - `georoute_last_success_unixtime{country}`
  - `georoute_cache_age_seconds{country}`
  - `georoute_fetch_duration_seconds{country, source}`
  - `georoute_nft_apply_duration_seconds{country}`
  - `georoute_frr_reload_duration_seconds{country}`

### Добавлено — Structured logs + run_id (#11)
- `--log-format=text|json` (по умолчанию `text`). JSON —
  один-запись-в-строке для systemd-journald structured ingest.
- `--log-level=debug|info|warn|error` (по умолчанию `info`).
- Каждая запись лога несёт `country`; для cycle-scoped строк —
  per-cycle `run_id` (UUIDv4). Операторы могут резать journald /
  Loki по циклу и джойнить к `/metrics` через exemplar-поддержку
  (когда она landет).

### Добавлено — Multi-country (#5)
- Флаг `--country=<ISO2>` (по умолчанию `RU`). Все `<cc>`-префиксные
  дефолты (имя сета, route-map, маркер-префикс, lock-path и т.д.)
  выводятся от него. Существующие RU-деплои работают без флага.

### Изменено
- systemd-template теперь использует `RuntimeDirectoryPreserve=yes`,
  чтобы flock-путь оставался стабильным между restart'ами;
  `StateDirectory=georoute` для кэша + last-success маркера;
  `ReadWritePaths=/run/frr` чтобы `frr-reload.py` мог писать
  reload-XXXXXX.txt temp-файл под `ProtectSystem=strict`.

### Миграция
- v1 → v2 upgrade in-place: `.env` переписывать не нужно. Новые
  флаги по умолчанию пустые / pre-v1 значения. Раскатать новый
  бинарь, потом opt-in к каждой фиче по готовности.

[2.0.0]: https://github.com/dantte-lp/georoute/releases/tag/v2.0.0

## [1.0.0] - 2026-06-15

### Добавлено
- Первая реализация: получение страновго списка от RIPE Stat, агрегация
  CIDR, атомарное splice'ние в FRR `frr.conf` между маркерами,
  атомарный `nft -f -` для замены сетов `inet pbr` v4/v6.
- CLI-флаги: `--dry-run`, `--force`, `--nft=false`, `--reload=false`,
  `--frr-conf=PATH`.
- Жёсткий конфиг golangci-lint v2 (`default: all` минус стилистические
  линтеры); ноль открытых замечаний на первом релизе.
- systemd `georoute.service` (oneshot) + `georoute.timer`
  (`OnBootSec=5min`, `OnUnitActiveSec=12h`, `RandomizedDelaySec=30min`).
- Каркас nftables-таблицы `inet pbr` (interval-сеты, цепочки prerouting + output).
- Сниппет FRR в `deploy/examples/frr-snippet.conf`, показывающий где должны
  жить маркеры `! BEGIN-RU-FEED-V4` / `! END-RU-FEED-V4` (и v6).

### Известные ограничения
- Страна и источник фида хардкодены — `RU` / RIPE Stat (снято в v2).
- Только single-binary; нет агентно-серверной модели.
- Имена nft-сетов (`ru_v4` / `ru_v6`) — захардкожены; смена требует патча
  бинаря (снято в v2).

[1.0.0]: https://github.com/dantte-lp/georoute/releases/tag/v1.0.0
