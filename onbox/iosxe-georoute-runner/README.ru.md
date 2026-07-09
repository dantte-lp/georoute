# IOS XE georoute runner

`iosxe-georoute-runner` — заготовка on-box приложения для C8000V IOS XE Application
Hosting. Приложение формирует scoped candidate-конфигурацию RU-префиксов и отдаёт её
локальному IOS XE через HTTP endpoint для последующего `copy ... running-config`.

## Ограничения Sprint 1

- live apply не выполняется;
- SSH paste не используется;
- candidate содержит только управляемые объекты `PL-GEOROUTE-RU-V4`,
  `RM-GLOBAL-TO-TRANSIT`, `RM-RU-GEOROUTE-OUT`, `RM-GEOROUTE-RU-STATIC` и static routes
  с `tag 201 name GEOROUTE_RU`;
- redistribution route-map для static routes использует `match tag 201`; prefix-list используется
  как guardrail и для outbound-policy;
- feed не должен содержать `0.0.0.0/0`, RFC1918, RFC6598 и внутренний блок `10.64.0.0/15`.

## Проверка

```bash
uv lock --check
uv sync --locked --dev
uv run ruff format --check
uv run ruff check
uv run ty check
uv run pytest
```
