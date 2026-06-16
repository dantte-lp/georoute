# Изменения

Все значимые изменения этого проекта документируются здесь.
Формат — [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/),
версионирование — [SemVer](https://semver.org/lang/ru/spec/v2.0.0.html).

## [Unreleased]

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
- Страна и источник фида хардкодены — `RU` / RIPE Stat. См. roadmap в
  [docs/ARCHITECTURE.ru.md](docs/ARCHITECTURE.ru.md#roadmap).
- Только single-binary; нет агентно-серверной модели.
- Имена nft-сетов (`ru_v4` / `ru_v6`) — захардкожены; смена требует патча
  бинаря.

[Unreleased]: https://github.com/dantte-lp/georoute/commits/main
