# Справочник по конфигурации

`georoute` конфигурируется только флагами. Дефолты подобраны для узла, на
котором работает FRR с integrated `frr.conf`, есть nftables-бэкендный
firewalld (так что таблица `inet pbr` — независимая) и который готов
ходить за фидом в RIPE Stat.

## Флаги

| Флаг | По умолчанию | Эффект |
|---|---|---|
| `--frr-conf` | `/etc/frr/frr.conf` | Путь к FRR-конфигу с маркерами. |
| `--reload` | `true` | Запускать `frr-reload.py --reload <frr-conf>` после успешной записи. `false` — для staged-деплоя. |
| `--nft` | `true` | Атомарно заменить содержимое `inet pbr ru_v4` / `ru_v6` через `nft -f -`. `false` — пропустить data-plane. |
| `--dry-run` | `false` | Скачать, агрегировать, напечатать сэмпл и предполагаемые числа. Ничего не менять. Взаимоисключающий с `--reload` и запись-семантиками. |
| `--force` | `false` | Применить даже если отрендеренный BGP-блок совпадает с тем, что уже на диске. Полезно после ручной правки `frr.conf`. |

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
