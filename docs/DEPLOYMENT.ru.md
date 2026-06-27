# Деплой

Как поставить `georoute` на exit-узел с нуля.

## Пререквизиты

- Linux, ядро ≥ 5.10 (мы тестируем на 6.12 LTS, UEK R8).
- nftables (с firewalld или standalone — оба работают; таблица `inet pbr`
  живёт независимо от firewalld'овой `inet firewalld`).
- FRR ≥ 10.1, integrated `frr.conf`, `bgpd` включён.
- Go ≥ 1.26 — только для сборки из исходников; релизный бинарь статический.
- Существующий BGP-пиринг с тем узлом, который должен получать страновые
  префиксы (mesh-пир, route reflector, апстрим — нам всё равно, главное
  что route-map подключён).

## Шаг 1 — поставить бинарь

Из release-tarball'а:

```bash
curl -fsSLO https://github.com/dantte-lp/georoute/releases/latest/download/georoute-linux-amd64
sudo install -m 0755 georoute-linux-amd64 /usr/local/bin/georoute
georoute --dry-run
```

Из исходников:

```bash
git clone https://github.com/dantte-lp/georoute.git
cd georoute
make install   # собирает, ставит в /usr/local/bin/georoute
```

## Шаг 2 — добавить каркас nftables

Файл копируется один раз; последующие правки его содержимого — на стороне
оператора, `georoute` его не перезаписывает.

```bash
sudo install -d /etc/nft.d
sudo install -m 0644 deploy/nftables/pbr.nft /etc/nft.d/pbr.nft
sudo nft -f /etc/nft.d/pbr.nft
sudo nft list table inet pbr   # smoke-test
```

Скорее всего вам понадобится systemd-юнит, чтобы это грузилось на boot:

```ini
# /etc/systemd/system/nft-pbr.service
[Unit]
Description=Load policy-routing nftables scaffolding for georoute
Before=network-pre.target
DefaultDependencies=no

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/nft -f /etc/nft.d/pbr.nft
ExecStop=/usr/sbin/nft delete table inet pbr

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now nft-pbr.service
```

## Шаг 3 — добавить `ip rule` + таблицу 100

Это завязано на конкретное окружение (какой uplink, какой gateway):

```bash
sudo ip -4 rule add fwmark 0x201 lookup 100 priority 100
sudo ip -6 rule add fwmark 0x201 lookup 100 priority 100
sudo ip -4 route add default via 91.218.113.129 dev ens1 table 100
sudo ip -6 route add default dev sit1 table 100
```

Persist через systemd (one-shot-юнит с `After=network-online.target`).
См. [examples/pbr-ru-exit.service](../deploy/systemd/) для паттерна.

## Шаг 4 — отредактировать `frr.conf`

Откройте `/etc/frr/frr.conf` и:

1. Добавьте community-list, route-map и outbound route-map permits из
   [examples/frr-snippet.conf](../deploy/examples/frr-snippet.conf).
2. Добавьте две пары маркеров внутрь блоков `address-family ipv4 unicast`
   и `ipv6 unicast`. Отступ — два пробела, как у окружающих `network`-строк.

Перезагрузите FRR один раз (убедитесь, что route-map распознан):

```bash
sudo /usr/lib/frr/frr-reload.py --test /etc/frr/frr.conf
sudo /usr/lib/frr/frr-reload.py --reload /etc/frr/frr.conf
sudo vtysh -c 'show route-map MARK-RU-EXIT'
```

## Шаг 5 — первый запуск фида (dry-run, потом real)

```bash
sudo georoute --dry-run
sudo georoute
```

После реального запуска:

```bash
nft list set inet pbr ru_v4 | head
nft list set inet pbr ru_v6 | head
vtysh -c 'show bgp ipv4 unicast | grep "Total"'
ip route show table 100
```

Сеты `inet pbr` должны иметь тысячи элементов; версия таблицы FRR должна
вырасти; таблица 100 по-прежнему держит только ваш локальный default.

## Шаг 6 — поставить systemd-таймер

```bash
sudo make install-systemd
systemctl list-timers georoute.timer
journalctl -u georoute.service -n 50
```

Таймер срабатывает на `OnBootSec=5min`, дальше каждые `12h` с
`30min`-jitter'ом (см. [deploy/systemd/georoute.timer](../deploy/systemd/georoute.timer)).

## Проверка

Корректно развёрнутый `georoute` должен дать:

- `nft list table inet pbr` — непустые сеты `ru_v4` и `ru_v6`.
- `vtysh -c 'show bgp ... summary'` — пир up, `PfxSnt` отражает фид.
- Для трафика VPN-клиента на country-IP: `ip route get <ip>` показывает
  ожидаемый локальный nexthop из таблицы 100 *только если запущен с*
  `mark 0x201`. Без mark'а lookup проваливается в main FIB (BGP-default
  или что там у вас).
- `journalctl -u georoute.service` показывает хеши, которые меняются
  только когда меняется upstream-фид.

## Привилегии

`georoute` нужны:

- HTTPS к `stat.ripe.net`. Никаких особых cap.
- Запись `/etc/frr/frr.conf` (root или member группы `frr` с `0640`).
- Exec `/usr/lib/frr/frr-reload.py` (root).
- `nft -f -` (CAP_NET_ADMIN).

Поставляемый systemd-юнит запускает сервис как `root`, sandbox'нутый
`NoNewPrivileges`, `ProtectSystem=strict` и плотным capability-сетом
(`CAP_NET_ADMIN`, `CAP_NET_RAW`). Запуск под менее привилегированным
пользователем требует делегирования этих cap'ов и записи в `/etc/frr` —
возможно, но не дефолт.

## Daemon-режим (v2.1+)

Вместо `Type=oneshot` + 12-часовой таймер `georoute` может крутиться
как long-lived `Type=simple` сервис. Внутренний тикер
`--refresh-interval` заменяет systemd-таймер.

Когда выбирать daemon:
- Нужен стабильный target для скрейпа `/metrics`.
- Нужны `/live` и `/ready` для внешнего оркестратора.
- Хочется per-cycle `run_id` корреляции в journald.

Когда oneshot ок:
- Single-node, без оркестратора.
- Предпочитаете, чтобы unit "падал видимо в journalctl" между циклами, а не следить за long-lived процессом.

### Переключение в daemon

1. Снести таймер:

   ```bash
   systemctl disable --now georoute@ru.timer
   ```

2. В `/etc/georoute/ru.env` добавить daemon-only переменные:

   ```env
   GEOROUTE_HTTP_ADDR=127.0.0.1:9494
   GEOROUTE_LOG_FORMAT=json
   GEOROUTE_LOG_LEVEL=info
   GEOROUTE_REFRESH_INTERVAL=12h
   ```

3. Поправить `/etc/systemd/system/georoute@.service`:

   ```ini
   Type=simple
   Restart=on-failure
   RestartSec=5s
   TimeoutStopSec=15s
   EnvironmentFile=/etc/georoute/%i.env
   ExecStart=/usr/local/bin/georoute \
       ... existing flags ... \
       --http-addr=${GEOROUTE_HTTP_ADDR} \
       --log-format=${GEOROUTE_LOG_FORMAT} \
       --log-level=${GEOROUTE_LOG_LEVEL} \
       --refresh-interval=${GEOROUTE_REFRESH_INTERVAL}
   ```

4. Reload + start:

   ```bash
   systemctl daemon-reload
   systemctl enable --now georoute@ru.service
   ```

5. Проверка:

   ```bash
   curl -sf http://127.0.0.1:9494/live      # 200
   curl -sf http://127.0.0.1:9494/ready     # 200 после первого успешного цикла
   curl -sf http://127.0.0.1:9494/metrics | grep georoute_runs_total
   ```

### Выбор порта

Default healthcheck-библиотеки — `:8080`; команда обычно использует
`:9090` (Prometheus-конвенция). **Выбирайте свободный порт per host** —
`9090` часто занят другими observability-сервисами (crowdsec-ocserv-bouncer
на dev-04, например). Биндите на `127.0.0.1:<port>`, если scrape target
не должен ходить в LAN; иначе ставьте reverse proxy впереди и держите
сам бинарь на localhost.

### Source-side пример

Канонический unit-файл — в
[`deploy/systemd/georoute@.service`](../deploy/systemd/georoute@.service),
с daemon-режим diff'ом в комментарии в конце для copy-paste. Ansible
role в `polyexit-prod` рендерит оба варианта из одного шаблона через
`georoute_mode: oneshot | daemon`.
