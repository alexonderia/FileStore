# Этап 0: контракт и компилируемый каркас — ручная проверка

## Цель и границы

Проверить, что из чистого checkout собираются и запускаются API и CLI, конфигурация имеет ожидаемый приоритет, health endpoints отвечают, ошибки имеют единый problem format, а OpenAPI проходит validation. В актуальном репозитории более поздние product endpoints уже существуют, поэтому stage-0 режим воспроизводится явным запуском без PostgreSQL.

## Предусловия

- Git.
- Go 1.25+ либо Docker 24+.
- Node.js 22+ нужен только для локального OpenAPI lint; без Node команду можно выполнить через CI.
- Свободный TCP-порт 18081 (либо другой явно выбранный тестовый порт).

Все команды выполняются из корня репозитория. В примерах нет реальных паролей или tokens.

## Автоматические проверки

С установленным Go:

```text
go fmt ./...
go vet ./...
go test -race ./...
go build -o bin/filestore-api ./cmd/filestore-api
go build -o bin/filestore ./cmd/filestore
npx --yes @redocly/cli@1.34.3 lint openapi/openapi.yaml
```

Если `go test -race ./...` локально на Windows завершается сообщением `-race requires cgo`, это не считается дефектом проекта. В таком случае:

```text
go test ./...
```

достаточно для локальной приемки, а `-race` должен оставаться зеленым в CI или в отдельном окружении с настроенным `cgo`.

Без локального Go:

```text
docker run --rm -v "${PWD}:/src" -w /src golang:1.25-alpine go test ./...
docker build --target api -t filestore-api:stage-0 .
docker build --target cli -t filestore-cli:stage-0 .
```

Ожидаемый результат: `gofmt`, `go vet`, обычные unit tests и build-команды завершаются с кодом 0, OpenAPI не содержит ошибок. `go test -race ./...` обязателен для CI; локально на Windows без `cgo` допустима замена на `go test ./...`.

## Позитивный сценарий

1. Запустить API:

   ```text
   go run ./cmd/filestore-api --listen=:18081 --database-url=postgres://filestore:filestore-local@localhost:5432/filestore?sslmode=disable
   ```

2. Проверить `GET http://localhost:18081/health/live`. Ожидаются HTTP 200, `Content-Type: application/json` и `{"status":"live"}`.
3. Проверить `GET http://localhost:18081/health/ready`. Ожидаются HTTP 200 и `{"status":"ready"}`.
4. Выполнить `go run ./cmd/filestore help`. В выводе должны присутствовать `version` и `config get/set`.
5. Назначить временный путь конфигурации через `FILESTORE_CONFIG`, затем выполнить:

   ```text
   go run ./cmd/filestore config set api-url http://localhost:18081
   go run ./cmd/filestore config set workspace 00000000-0000-0000-0000-000000000001
   go run ./cmd/filestore config get
   ```

6. Убедиться, что CLI показывает оба значения, а JSON-файл создан только по указанному временному пути.
7. Остановить API через Ctrl+C. Ожидается корректное завершение без panic и сообщение `FileStore API stopped`.

## Негативные проверки

1. Запустить API с `--listen=`. Ожидается код 2 и сообщение о некорректной конфигурации.
2. Запустить API с `FILESTORE_API_SHUTDOWN_TIMEOUT=invalid`. Ожидается код 2 без запуска listener.
3. Выполнить `filestore unknown`. Ожидается код 2, понятная ошибка и help.
4. Выполнить `filestore config set api-url not-a-url`. Ожидается отказ; некорректное значение не сохраняется.
5. Запросить `GET /missing`. Ожидаются HTTP 404, `application/problem+json`, поля `type`, `title`, `status`, `code` и `instance`.
6. В API, запущенном с `--database-url=`, запросить product endpoint, например `POST /auth/login`. Он должен вернуть problem `404`, потому что composition root не подключает product routes без БД. При обычном запуске с PostgreSQL этот endpoint реализован последующим этапом D-02.

## Проверка приоритета конфигурации

1. Установить `FILESTORE_API_LISTEN=:8081` и запустить API без флага: listener должен использовать 8081.
2. Не снимая переменную, запустить с `--listen=:8082`: listener должен использовать 8082.
3. Удалить переменную и задать `FILESTORE_API_LISTEN=:8081` во временном `.env`: должен использоваться 8081.
4. Удалить значение из `.env` и флаг: должен использоваться default `:8080`.

Фактический порядок приоритета: flags → environment → `.env` → defaults. Автоматический тест `TestDotEnvThenEnvironmentThenFlagsPrecedence` проверяет всю цепочку без изменения рабочего `.env`.

## Очистка

- Остановить API.
- Удалить временный CLI config и переменные `FILESTORE_CONFIG`, `FILESTORE_API_LISTEN`, `FILESTORE_API_SHUTDOWN_TIMEOUT`.
- При необходимости удалить локальные `bin/`, `coverage.out` и образы `filestore-*:stage-0`; эти файлы уже исключены через `.gitignore`.

## Итоговый checklist

- [x] `go fmt`, `go vet`, unit tests и build завершились успешно.
- [x] Локальный Windows `-race` недоступен из-за `cgo`; обычные tests прошли, а полный `go test -race ./...` успешно выполнен в `golang:1.25-bookworm` и остаётся обязательным в CI.
- [x] OpenAPI lint завершился без ошибок.
- [x] Обе Docker build target (`api`, `cli`) собраны.
- [x] Оба health endpoint вернули ожидаемые ответы.
- [x] Неизвестный маршрут вернул RFC 9457 problem response.
- [x] CLI help/version/config работают.
- [x] Проверен приоритет flags → environment → `.env` → defaults.
- [x] Некорректная конфигурация отклоняется до запуска API.
- [x] Product endpoints не подключаются в stage-0 режиме без БД.
- [x] SIGTERM/context cancellation приводит к graceful shutdown и записи `FileStore API stopped`.
- [x] В tracked tree не появились binaries, local config, `.env`, logs или другие runtime-артефакты; временный config удалён, `bin/` остаётся ignored build output.

## Результат проверки

- Дата: 2026-07-16
- Проверил: Codex совместно с локальным окружением проекта
- Commit: working tree поверх `3421a2c`
- Окружение: Windows amd64; Go 1.26.5 (module baseline 1.25); Node.js 24.15.0; Docker 29.5.3
- Результат: пройдено
- Замечания: локальный Windows `go test -race ./...` требует отсутствующий C toolchain/cgo; race suite успешно пройдена в Linux-контейнере `golang:1.25-bookworm` и сохранена в CI. Ручной HTTP-сценарий выполнен контейнером `filestore-api:stage-0` на порту 18081 без БД; контейнер после проверки удалён.
