# IOS XE georoute runner

`iosxe-georoute-runner` — заготовка on-box приложения для C8000V IOS XE Application
Hosting. Приложение формирует scoped candidate-конфигурацию RU-префиксов и отдаёт её
локальному IOS XE через HTTP endpoint для последующего `copy ... running-config`.

## Ограничения PoC

- production RU feed не применяется без отдельного controlled test;
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

## Сборка package

```bash
make package
```

Результат:

- `dist/georoute-runner.tar` — container image tar для передачи на `bootflash:`;
- `dist/georoute-runner.tar.sha256` — контрольная сумма package.
- `dist/` является build artifact и не коммитится.

IOS XE lifecycle:

```ios
app-hosting install appid georoute-runner package bootflash:georoute-runner.tar
app-hosting activate appid georoute-runner
app-hosting start appid georoute-runner
show app-hosting list
```

Шаблон конфигурации сети и ресурсов: `iosxe-config.example.ios`.

Smoke-test перед переносом на C8000V:

```bash
printf '2.60.0.0/14\n' > /tmp/georoute-prefixes.txt
podman run --rm \
  -v /tmp/georoute-prefixes.txt:/data/prefixes.txt:Z \
  localhost/iosxe-georoute-runner:0.1.0 render
podman run --rm -p 127.0.0.1:18080:8080 \
  -v /tmp/georoute-prefixes.txt:/data/prefixes.txt:Z \
  localhost/iosxe-georoute-runner:0.1.0 serve
```
