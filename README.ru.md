# georoute

> Синхронизация BGP-анонсов и nftables PBR по странам для гео-разделённых exit-нод.

`georoute` — небольшой Go-демон, который держит в согласии control-plane (BGP)
и data-plane (nftables), когда вы маршрутизируете по стране назначения.
Тянет каноничный список префиксов страны из RIPE Stat, агрегирует его до
минимального покрывающего набора CIDR и атомарно обновляет:

1. **FRR** — вставляет строки `network X.X.X.X/Y route-map MARK-RU-EXIT`
   между специальными маркерами в `frr.conf` и затем триггерит
   `frr-reload.py` *(no-op, если диф пустой)*.
2. **nftables** — атомарно заменяет содержимое сетов `inet pbr ru_v4` и
   `inet pbr ru_v6` одной транзакцией `nft -f -`.

Отдельное `ip rule fwmark 0x201 lookup 100` + выделенная таблица 100
заставляют совпавший трафик выходить через локальный uplink, а не через
дефолтный маршрут от BGP-пира. Главная FIB остаётся компактной (никаких
8 000+ статических маршрутов), а BGP продолжает анонсировать страновые
префиксы партнёру, чтобы тот завернул соответствующий трафик в этот узел.

## Почему не статические маршруты

Static-route + `redistribute static` — очевидный подход, но он засоряет
основной FIB тысячами записей и смешивает «вот путь для данных» с «вот
что я хочу, чтобы узнали пиры». `georoute` строго разводит эти два:

| Что | Чем |
|---|---|
| Анонсить префиксы BGP-пирам | FRR `network X` + `no bgp network import-check` |
| Форвардить локально-порождённые пакеты | nftables interval-set + `fwmark` + policy routing |
| Форвардить транзитные пакеты | то же — chain цепляется и за `prerouting`, и за `output` |
| Пережить отказ BGP | локальный kernel-маршрут в выделенной таблице независим |

nftables interval-set индексирован деревом (O(log n) на lookup), так что
стоимость data-plane постоянна вне зависимости от размера фида.

## Статус

Pre-1.0. Внутренняя инфра-утилита. Production-ready для паттерна
двух-сайтового exit, описанного в [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
Выбор страны сейчас хардкоден на `RU` против RIPE Stat; сделать источник/страну
подключаемыми — в плане.

## Быстрый старт

Сборка (нужен Go ≥ 1.26):

```bash
make build              # собирает ./georoute
make install            # ставит в /usr/local/bin/georoute
make install-systemd    # ставит service + timer в /etc/systemd/system
```

Однократный запуск (идемпотентный, FRR не трогает если ничего не изменилось):

```bash
georoute
```

Dry-run (тянет и агрегирует, печатает сэмпл, ничего не пишет):

```bash
georoute --dry-run
```

Принудительная запись/reload, даже если хеши блоков BGP не поменялись:

```bash
georoute --force
```

Полный список флагов — в [docs/CONFIGURATION.md](docs/CONFIGURATION.md);
остальное по инфраструктуре (nftables-каркас, `ip rule`, FRR-маркеры) —
в [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

## Модель работы

```
                   RIPE Stat (HTTPS)
                         │
                         ▼
                    ┌──────────┐
                    │ georoute │  ── триггерится таймером, OnUnitActiveSec=12h
                    └──────────┘
                       │      │
              ┌────────┘      └─────────┐
              ▼                          ▼
     ┌─────────────────┐         ┌──────────────────┐
     │  /etc/frr/      │         │  nft set         │
     │  frr.conf       │         │  inet pbr ru_v4  │
     │  (маркеры)      │         │  inet pbr ru_v6  │
     └─────────────────┘         └──────────────────┘
              │                          │
              ▼                          ▼
     frr-reload.py             ядро (data plane)
              │                          │
              ▼                          ▼
     BGP UPDATE пиру           mark 0x201 → table 100
                                         │
                                         ▼
                              default через локальный uplink
```

## Структура репозитория

```
.
├── main.go               # всё содержимое утилиты, осознанно один файл
├── go.mod
├── Makefile
├── deploy/
│   ├── systemd/          # service + timer
│   ├── nftables/         # каркас для `inet pbr`
│   └── examples/         # сниппет frr.conf с маркерами
├── docs/
│   ├── ARCHITECTURE.md   # control vs. data plane, почему nftables+PBR
│   ├── DEPLOYMENT.md     # системные пререквизиты, ip rule, table 100, timer
│   ├── CONFIGURATION.md  # каждый флаг, маркер, exit code
│   └── RUNBOOK.md        # day-2: ротация ключей, восстановление после дрейфа
└── .github/              # CI, шаблоны issues, dependabot
```

## Документация

- 🇬🇧 English (источник правды): см. файлы выше.
- 🇷🇺 Русский (канонический перевод): теми же именами с суффиксом `.ru.md`
  (например, [README.md](README.md)).

## Лицензия

Proprietary. См. [LICENSE](LICENSE).
