# Справочник по конфигурации

`georoute` конфигурируется только флагами. Дефолты подобраны для узла, на
котором работает FRR с integrated `frr.conf`, есть nftables-бэкендный
firewalld (так что таблица `inet pbr` — независимая) и который готов
ходить за фидом в RIPE Stat.

## Флаги

### Ядро (v1)

| Флаг | По умолчанию | Эффект |
|---|---|---|
| `--frr-conf` | `/etc/frr/frr.conf` | Путь к FRR-конфигу с маркерами. |
| `--reload` | `true` | Запускать `frr-reload.py --reload <frr-conf>` после успешной записи. |
| `--nft` | `true` | Атомарно заменить содержимое `inet pbr <cc>_v4` / `<cc>_v6` через `nft -f -`. |
| `--dry-run` | `false` | Скачать, агрегировать, напечатать сэмпл. Ничего не менять. |
| `--force` | `false` | Применить даже если хэш блока совпадает. |
| `--lock-file` | `/run/georoute-<cc>.lock` | Путь к flock'у; параллельные запуски быстро отвалятся. |

### Multi-country (v2.0)

| Флаг | По умолчанию | Env | Эффект |
|---|---|---|---|
| `--country` | `RU` | `GEOROUTE_COUNTRY` | ISO-3166 alpha-2 код; все `<cc>` дефолты строятся от него. |
| `--feed-url` | URL RIPE | `GEOROUTE_FEED_URL` | Переопределить RIPE Stat endpoint. |
| `--route-map` | `MARK-<CC>-EXIT` | `GEOROUTE_ROUTE_MAP` | FRR route-map name в каждой `network` строке. |
| `--nft-set-v4` | `<cc>_v4` | `GEOROUTE_NFT_SET_V4` | Имя v4 nft-сета в `inet pbr`. |
| `--nft-set-v6` | `<cc>_v6` | `GEOROUTE_NFT_SET_V6` | Имя v6 nft-сета. |
| `--marker-prefix` | `<CC>-FEED` | `GEOROUTE_MARKER_PREFIX` | Префикс маркер-комментов; задаёт форму `BEGIN-<P>-V4` / `END-<P>-V4`. |

### Данные оператора + state (v2.0)

| Флаг | По умолчанию | Env | Эффект |
|---|---|---|---|
| `--extras-v4-file` | `""` | `GEOROUTE_EXTRAS_V4_FILE` | Список оператор-managed IPv4 префиксов, мёрджится с RIPE-фидом. Один префикс на строке, `#` — комменты. Пусто = нет extras. |
| `--extras-v6-file` | `""` | `GEOROUTE_EXTRAS_V6_FILE` | То же для IPv6. |
| `--cache-file` | `/var/lib/georoute/feed-<cc>.json.gz` | `GEOROUTE_CACHE_FILE` | Gzip+JSON снимок последнего успешного RIPE-ответа. Fallback при 5xx. |
| `--cache-max-age` | `168h` (7 дней) | `GEOROUTE_CACHE_MAX_AGE` | Максимальный возраст кэша. |
| `--last-success-file` | `/var/lib/georoute/last-success-<cc>` | `GEOROUTE_LAST_SUCCESS_FILE` | Timestamp успешного цикла; `/readyz` смотрит сюда. |

### Наблюдаемость (v2.0 — opt-in через `--http-addr`)

| Флаг | По умолчанию | Env | Эффект |
|---|---|---|---|
| `--http-addr` | `""` | `GEOROUTE_HTTP_ADDR` | Listen-адрес для `/live`, `/ready`, `/metrics`, `/debug/pprof/*`. Пусто отключает сервер. |
| `--ready-max-age` | `24h` | `GEOROUTE_READY_MAX_AGE` | Возраст last-success, при котором `/readyz` начинает возвращать 503. |
| `--log-format` | `text` | `GEOROUTE_LOG_FORMAT` | `text` (читаемо) или `json` (одна запись на строку для systemd-journald). |
| `--log-level` | `info` | `GEOROUTE_LOG_LEVEL` | `debug`, `info`, `warn`, `error`. |

### Tunable таймауты + пути к утилитам (v2.1)

| Флаг | По умолчанию | Env | Эффект |
|---|---|---|---|
| `--http-timeout` | `60s` | `GEOROUTE_HTTP_TIMEOUT` | Per-request таймаут для RIPE Stat fetch'а. |
| `--frr-reload-timeout` | `3m` | `GEOROUTE_FRR_RELOAD_TIMEOUT` | Wall-budget для `frr-reload.py`. |
| `--nft-timeout` | `30s` | `GEOROUTE_NFT_TIMEOUT` | Budget для `nft -f -`. |
| `--retry-attempts` | `3` | `GEOROUTE_RETRY_ATTEMPTS` | Число попыток RIPE Stat fetch'а до fallback'а на кэш. |
| `--retry-base-delay` | `2s` | `GEOROUTE_RETRY_BASE_DELAY` | Базовый линейный backoff; N-я retry ждёт `(N-1) * delay`. |
| `--frr-reload-script` | `/usr/lib/frr/frr-reload.py` | `GEOROUTE_FRR_RELOAD_SCRIPT` | Override для других дистрибутивов. |
| `--nft-binary` | `/usr/sbin/nft` | `GEOROUTE_NFT_BINARY` | Override для musl/alt-инсталляций. |
| `--refresh-interval` | `0` (oneshot) | `GEOROUTE_REFRESH_INTERVAL` | `> 0` + `--http-addr` set: daemon-режим. Пайплайн крутится на этом интервале. |

### Target-rendering (экспериментально)

Target-rendering позволяет использовать `georoute` как prefix-loader для
маршрутизаторов, на которых не запущен локальный FRR. `georoute` не становится
демоном маршрутизации: он скачивает, валидирует, агрегирует и рендерит
префиксные или policy-объекты. Целевой маршрутизатор отвечает за route origin,
протокольные сессии, forwarding, NAT и policy-based routing.

| Флаг | По умолчанию | Эффект |
|---|---|---|
| `--target` | `frr` | Target-адаптер. `frr` сохраняет прежний FRR + nftables apply-path. `file` и `iosxe` сейчас поддерживают только render-режим. |
| `--mode` | `apply` | Режим выполнения. Для target-адаптеров вне FRR используется `render`. |
| `--target-output` | `-` | Куда писать результат. `-` пишет в stdout; другое значение атомарно записывается в файл. |
| `--iosxe-prefix-list` | `PL-GEOROUTE-<CC>-V4` | Имя IPv4 prefix-list в IOS XE. |
| `--iosxe-route-map` | `RM-GEOROUTE-<CC>-OUT` | Имя route-map в IOS XE, который match'ит созданный prefix-list. |
| `--iosxe-route-map-seq` | `10` | Permit sequence в IOS XE route-map для match созданного prefix-list. Используйте свободную sequence при добавлении georoute в существующий route-map. |
| `--iosxe-route-map-deny-seq` | `9999` | Sequence явного deny в IOS XE route-map. |
| `--iosxe-replace-prefix-list` | `false` | Рендерит `no ip prefix-list <name>` перед строками IOS XE prefix-list. Используется, когда кандидат применяется как авторитетное состояние объекта и нужно удалить устаревшие entries. |
| `--iosxe-static-next-hop` | `""` | Next-hop для IOS XE static routes. Пустое значение отключает рендеринг static routes. |
| `--iosxe-static-route-map` | `RM-GEOROUTE-<CC>-STATIC` | IOS XE route-map для `redistribute static`. Default выставляется только при `--target=iosxe`. |
| `--iosxe-static-route-name` | `GEOROUTE_<CC>` | Имя IOS XE static route для сгенерированных маршрутов. Default выставляется только при `--target=iosxe`. |
| `--iosxe-static-tag` | `0` | Опциональный tag для IOS XE static route. `0` не рендерит tag. |
| `--iosxe-bgp-as` | `""` | IOS XE BGP AS для stanza `router bgp <asn> address-family ipv4 redistribute static route-map <name>`. Пусто отключает BGP stanza. |
| `--iosxe-apply-max-prefixes` | `100` | Safety-limit для будущего IOS XE apply-mode. Для полного feed лимит должен повышаться явно после реализации diff/postcheck. |
| `--iosxe-ssh-host` | `""` | IOS XE SSH host для apply-mode. Обязателен для `--target=iosxe --mode=apply`. |
| `--iosxe-ssh-port` | `22` | IOS XE SSH port для apply-mode. |
| `--iosxe-ssh-user` | `""` | IOS XE SSH username для apply-mode. Обязателен. |
| `--iosxe-ssh-key-file` | `""` | IOS XE SSH private key file для apply-mode. Обязателен. |
| `--iosxe-known-hosts-file` | `""` | Файл `known_hosts` для проверки host key IOS XE. Обязателен; insecure host-key ignore не поддерживается. |
| `--iosxe-ssh-timeout` | `30s` | SSH dial/session timeout для IOS XE apply-mode. |
| `--iosxe-save-config` | `false` | Выполнить `write memory` после успешного IOS XE apply postcheck. |

Примеры:

```bash
georoute --country=RU --target=file --mode=render --target-output=ru-prefixes.txt
georoute --country=RU --target=iosxe --mode=render --target-output=iosxe-ru.cfg
georoute --country=RU --target=iosxe --mode=render --iosxe-replace-prefix-list --target-output=iosxe-ru.cfg
georoute --country=RU --target=iosxe --mode=render --iosxe-replace-prefix-list --iosxe-route-map=RM-RU-GEOROUTE-OUT --iosxe-route-map-seq=20 --target-output=iosxe-ru-c8000v.cfg
georoute --country=RU --target=iosxe --mode=render --iosxe-replace-prefix-list --iosxe-route-map=RM-RU-GEOROUTE-OUT --iosxe-route-map-seq=20 --iosxe-static-next-hop=94.139.250.25 --iosxe-bgp-as=4200099722 --target-output=iosxe-ru-c8000v-static-origin.cfg
```

IOS XE renderer добавляет явный tail `deny 0.0.0.0/0 le 32` в prefix-list и
явный `route-map ... deny 9999`. Без `--iosxe-replace-prefix-list` IOS XE
оставляет старые sequence entries, если следующий feed стал меньше. Для C8000V
включайте replace-mode и применяйте кандидат в контролируемой config-транзакции.

Static-origin rendering является опциональным. Если задан
`--iosxe-static-next-hop`, кандидат содержит по одной строке
`ip route <prefix> <mask> <next-hop>` на каждый агрегированный IPv4 prefix. Если
также задан `--iosxe-bgp-as`, кандидат содержит отдельный static route-map и
stanza `redistribute static route-map`. Используйте этот режим только когда
целевой маршрутизатор должен создавать route origin для feed.

IOS XE apply-mode открывает SSH-сессию с public-key authentication, проверяет
host key маршрутизатора через `known_hosts`, отправляет сгенерированный кандидат
в configuration mode, затем выполняет show-based postcheck для prefix-list,
route-map и опциональной static redistribution stanza. Default
`--iosxe-apply-max-prefixes=100` намеренно блокирует полный production feed,
пока оператор явно не повысит лимит.

## Маркеры в `frr.conf`

`georoute` пишет строки `network X/Y route-map MARK-RU-EXIT` между
маркерами. Маркеры должны существовать в файле до первого запуска:

```
! BEGIN-RU-FEED-V4
! END-RU-FEED-V4
```

и

```
! BEGIN-RU-FEED-V6
! END-RU-FEED-V6
```

Они должны быть *внутри* блока `address-family ipv4 unicast` /
`address-family ipv6 unicast` в стацзе `router bgp`. Отступ важен: два
ведущих пробела, как у окружающих `network`-строк. См.
[deploy/examples/frr-snippet.conf](../deploy/examples/frr-snippet.conf).

## Ожидания со стороны data-plane

`georoute` предполагает, что статический каркас на узле уже стоит:

1. Существует nftables-таблица `inet pbr` с сетами `ru_v4` и `ru_v6`
   (тип `ipv4_addr` / `ipv6_addr`, `flags interval`). Используйте
   [deploy/nftables/pbr.nft](../deploy/nftables/pbr.nft) один раз при
   установке.
2. В ядре есть `ip rule`, направляющее `fwmark 0x201` в нумерованную
   таблицу (по умолчанию в наших примерах — `100`):

   ```bash
   ip -4 rule add fwmark 0x201 lookup 100 priority 100
   ip -6 rule add fwmark 0x201 lookup 100 priority 100
   ```
3. Таблица 100 содержит *локальный* default — куда должен уйти
   помеченный трафик:

   ```bash
   ip -4 route add default via <local-uplink-gw> dev <iface> table 100
   ip -6 route add default dev <v6-iface> table 100
   ```

Сам `georoute` трогает только *содержимое* сетов; каркас выше — install-time,
остаётся на месте.

## Exit-коды

| Код | Значение |
|---|---|
| 0 | Успех. Либо ничего не поменялось и мы пропустили, либо изменение применено. |
| 1 | Какой-то шаг упал (fetch, aggregate, splice, write, reload). Смотрите stderr / journald. |

## Идемпотентность

Запуск, который даёт тот же рендер, что прошлый, *не* перезаписывает
`frr.conf` и *не* вызывает `frr-reload.py`. nft-обновление выдаётся
всегда (это одна транзакция; цена пренебрежимая), и сам `nft` идемпотентен —
замена сета на идентичное содержимое — no-op относительно data-plane.

Поэтому `OnUnitActiveSec=12h` обходится дёшево: ничего не происходит,
если фид не изменился.

## Режимы отказа

- **RIPE Stat недоступен.** Запуск завершается кодом 1. Следующий
  scheduled-запуск (timer) попробует ещё раз. Никакого
  полу-применённого состояния.
- **Отсутствуют маркеры в `frr.conf`.** Запуск падает с `errBeginMissing` /
  `errEndMissing`. Добавьте маркеры (см. выше) и перезапустите.
- **`nft -f -` упал.** Скорее всего таблица или сет не существуют.
  Загрузите [deploy/nftables/pbr.nft](../deploy/nftables/pbr.nft) один раз
  и попробуйте ещё раз.
- **Синтаксическая ошибка в `frr-reload.py`.** `georoute` уже записал
  новый `frr.conf`. Посмотрите, поправьте и либо откатите через
  `git checkout` (если у вас версионируется `/etc/frr/`), либо ручкой.
  Перезапустите с `--reload`, чтобы запушить.

## Структура исходников

Вся утилита — один `main.go`. Намеренная структура:

| Секция | Ответственность |
|---|---|
| `fetch` | HTTP `GET` в RIPE Stat, JSON-декод, базовая проверка формы. |
| `parsePrefixes` / `parseRange` | Принимает CIDR или `start-end` ranges из фида; нормализует в `netip.Prefix`. |
| `aggregate` | Сортировка, выбрасывание строгих подсетей, итеративное склеивание смежных одной длины. |
| `renderNetworks` | Эмитит строки `network X/Y route-map MARK-RU-EXIT`. |
| `splice` | Заменяет блок между маркерами в `frr.conf`. |
| `applyNft` | Собирает один `nft -f -` скрипт, который flush'ит оба сета и добавляет элементы. |
| `atomicWrite` | `<file>.new` + `rename`. |
| `reloadFRR` | Shell-out на `frr-reload.py --reload`. |

Добавить флаг — это добавить поля в `cliFlags`, спарсить в `realMain` и
пробросить через `run`. Плагин-системы, DI-контейнера и конфиг-файла
по дизайну нет.
