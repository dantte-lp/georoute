# Runbook — операции day-2

Процедуры для дежурного оператора.

## «BGP-пир не получает страновые префиксы»

Проверка цепочки end-to-end.

```bash
# 1. Sanity — georoute сегодня вообще запускался?
systemctl list-timers georoute.timer
journalctl -u georoute.service -n 20

# 2. Он записал блок?
grep -c "BEGIN-RU-FEED-V4" /etc/frr/frr.conf       # должно быть 1
sed -n '/BEGIN-RU-FEED-V4/,/END-RU-FEED-V4/p' /etc/frr/frr.conf | head

# 3. FRR держит network-строки?
vtysh -c 'show running-config' | grep -c 'route-map MARK-RU-EXIT'

# 4. BGP их видит в локальной таблице?
vtysh -c 'show bgp ipv4 unicast | grep -c "0.0.0.0"'    # ≫0 ожидаемо

# 5. Outbound route-map пира выпускает их наружу?
vtysh -c 'show bgp neighbor <peer-ip> advertised-routes | head'
```

Если шаг 4 ОК, а шаг 5 пустой: outbound-route-map (`TO-PEER`) на пире не
permit'ит community. Поправить, reload.

## «VPN-клиентский трафик на country-IP не использует локальный exit»

```bash
# 1. Адрес есть в сете?
nft get element inet pbr ru_v4 { 1.2.3.4 }   # не должно ругаться

# 2. Routing decision учитывает mark?
ip -4 route get 1.2.3.4 mark 0x201           # должен показать таблицу 100

# 3. На практике mark вообще ставится? (временный счётчик.)
nft 'add rule inet pbr prerouting ip daddr 1.2.3.4 counter comment trace'
# ... сгенерить трафик ...
nft list chain inet pbr prerouting | grep trace
nft -a list chain inet pbr prerouting    # найти handle
nft 'delete rule inet pbr prerouting handle <N>'
```

Если шаг 3 показал ноль hit'ов: цепочка не видит трафик. Типовые причины:

- **VRF double-pass.** Убедитесь, что `meta iifkind "vrf" return` есть на
  месте. Если ingress пакета — VRF-master, наша цепочка делает RETURN, и
  ниже работает zone-aware ruleset — но VRF-мастер тогда должен быть в
  пермиссивной firewalld-zone (например, `trusted`).
- **Reply-трафик.** `ct direction reply accept` намеренно исключает
  обратное направление; это правильно. Если действительно хочется метить
  и его (редко) — перенесите строку `ip daddr @ru_v4 ...` *выше*
  `ct direction`-фильтра.

## «Хочу принудительно ре-применить прямо сейчас»

```bash
sudo georoute --force
```

Это перерендерит даже если хеш блока BGP не поменялся, перепишет
`frr.conf`, перезапустит `frr-reload.py` и заново выдаст nft-транзакцию.

## «Стейджу изменение и пока reload не нужен»

```bash
sudo georoute --reload=false      # пишет frr.conf, применяет nft, без reload
# ... другие изменения ...
sudo /usr/lib/frr/frr-reload.py --reload /etc/frr/frr.conf
```

## «Источник фида неправильный / хочу соврать про принадлежность стране»

Вставьте ручной `static`-маршрут во FRR с тем же community, что использует
`MARK-RU-EXIT`, и kernel-маршрут в таблицу 100. Не правьте блок `georoute`
руками — ваша правка переживёт следующий запуск только если диф совпадёт
точно. Out-of-band исключения держите вне маркеров.

## «Хочу полностью отрубить страновой exit (временно)»

Проще всего — остановить timer, очистить сеты и блок маркеров.

```bash
sudo systemctl disable --now georoute.timer
sudo nft flush set inet pbr ru_v4
sudo nft flush set inet pbr ru_v6
# В frr.conf руками удалить все `  network ... route-map MARK-RU-EXIT`
# между маркерами (сами маркеры оставить).
sudo /usr/lib/frr/frr-reload.py --reload /etc/frr/frr.conf
```

Вернуть: `systemctl enable --now georoute.timer && georoute --force`.

## «Upstream-фид меня рейт-лимитит»

RIPE Stat либерален, но не бесконечно. 12-часовой schedule с 30-минутным
jitter'ом — в рамках бюджета. Если правда нужно чаще, замиррорьте данные
локально и направьте на mirror (фича-флаг под это появится; пока правьте
`ripeURL` в `main.go` и пересобирайте).

## «Внезапный скачок в `PfxSnt`»

Либо RIPE обновил датасет (allocation/reallocation бывают весомыми по
объёму), либо кто-то удалил `network`-директиву в другом месте
`router bgp`. Дифф текущий `frr.conf` против закоммиченной копии.

## Логи и где смотреть

| Компонент | Где |
|---|---|
| Запуски `georoute` | `journalctl -u georoute.service` |
| Вывод `frr-reload` | включён в вывод выше |
| Warning'и zebra/bgpd | `journalctl -u frr` |
| Лог nft | по дефолту нет — временно `nft 'add rule inet pbr prerouting ... log prefix "georoute: " level info'` |
| Outbound BGP UPDATE | `vtysh -c 'debug bgp updates'` (шумно — выключайте после) |
