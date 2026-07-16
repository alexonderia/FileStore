# Этап 0: контракт и компилируемый каркас — ручная проверка

## Цель и границы

Проверить, что из чистого checkout собираются и запускаются API и CLI, конфигурация имеет ожидаемый приоритет, health endpoints отвечают, ошибки имеют единый problem format, а OpenAPI проходит validation. Продуктовые endpoints на этом этапе только описаны в OpenAPI и не должны работать.

## Предусловия

- Git.
- Go 1.25+ либо Docker 24+.
- Node.js 22+ нужен только для локального OpenAPI lint; без Node команду можно выполнить через CI.
- Свободный TCP-порт 8080.

Все команды выполняются из корня репозитория. В примерах нет реальных паролей или tokens.

## Автоматические проверки

С установленным Go:

```text
gofmt -w .
go vet ./...
go test -race ./...
go build -o bin/filestore-api ./cmd/filestore-api
go build -o bin/filestore ./cmd/filestore
npx --yes @redocly/cli@1.34.3 lint openapi/openapi.yaml
```

Без локального Go:

```text
docker run --rm -v "${PWD}:/src" -w /src golang:1.25-alpine go test ./...
docker build --target api -t filestore-api:stage-0 .
docker build --target cli -t filestore-cli:stage-0 .
```

Ожидаемый результат: команды завершаются с кодом 0, unit tests зелёные, OpenAPI не содержит ошибок.

## Позитивный сценарий

1. Запустить API:

   ```text
   go run ./cmd/filestore-api
   ```

2. Проверить `GET http://localhost:8080/health/live`. Ожидаются HTTP 200, `Content-Type: application/json` и `{"status":"live"}`.
3. Проверить `GET http://localhost:8080/health/ready`. Ожидаются HTTP 200 и `{"status":"ready"}`.
4. Выполнить `go run ./cmd/filestore help`. В выводе должны присутствовать `version` и `config get/set`.
5. Назначить временный путь конфигурации через `FILESTORE_CONFIG`, затем выполнить:

   ```text
   go run ./cmd/filestore config set api-url http://localhost:8080
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
6. Запросить любой запланированный продуктовый endpoint, например `POST /auth/login`. На этапе 0 он не реализован и должен вернуть 404, а не фиктивный успешный ответ.

## Проверка приоритета конфигурации

1. Установить `FILESTORE_API_LISTEN=:8081` и запустить API без флага: listener должен использовать 8081.
2. Не снимая переменную, запустить с `--listen=:8082`: listener должен использовать 8082.
3. Удалить переменную и флаг: должен использоваться default `:8080`.

Порядок приоритета: flags → environment → defaults.

## Очистка

- Остановить API.
- Удалить временный CLI config и переменные `FILESTORE_CONFIG`, `FILESTORE_API_LISTEN`, `FILESTORE_API_SHUTDOWN_TIMEOUT`.
- При необходимости удалить локальные `bin/`, `coverage.out` и образы `filestore-*:stage-0`; эти файлы уже исключены через `.gitignore`.

## Итоговый checklist

- [ ] `gofmt`, `go vet`, unit tests и build завершились успешно.
- [ ] OpenAPI lint завершился без ошибок.
- [ ] Оба health endpoint вернули ожидаемые ответы.
- [ ] Неизвестный маршрут вернул RFC 9457 problem response.
- [ ] CLI help/version/config работают.
- [ ] Проверен приоритет flags → environment → defaults.
- [ ] Некорректная конфигурация отклоняется до запуска API.
- [ ] Продуктовые endpoints остаются неактивными.
- [ ] Ctrl+C приводит к graceful shutdown.
- [ ] В репозитории не появились binaries, local config, `.env`, logs или другие игнорируемые артефакты.

## Результат проверки

Заполняется вручную после выполнения:

- Дата:
- Проверил:
- Commit:
- Окружение:
- Результат: пройдено / не пройдено
- Замечания:
