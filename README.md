# FileStore

FileStore — это API-first сервис хранения файлов с версионностью, рабочими пространствами и управляемым совместным доступом. Репозиторий находится в активной разработке: сейчас готовы каркас API и CLI, контракт OpenAPI, базовая инфраструктура PostgreSQL и локальная bootstrap-команда для создания первого суперадминистратора.

Короткая точка входа в документацию:

- Общее описание проекта и текущего устройства: [docs/PROJECT_OVERVIEW.md](/C:/Users/alexonderia/Work/FileStore/docs/PROJECT_OVERVIEW.md)
- Технический план MVP: [docs/TECHNICAL_DESIGN_MVP.md](/C:/Users/alexonderia/Work/FileStore/docs/TECHNICAL_DESIGN_MVP.md)
- Ручная проверка этапа 0: [docs/stages/00-contract.md](/C:/Users/alexonderia/Work/FileStore/docs/stages/00-contract.md)
- Ручная проверка этапа 1: [docs/stages/01-identity-workspace.md](/C:/Users/alexonderia/Work/FileStore/docs/stages/01-identity-workspace.md)

## Что уже работает

- HTTP API с `health/live` и `health/ready`
- CLI с `help`, `version`, `config get`, `config set`
- единый формат ошибок `application/problem+json`
- контракт `openapi/openapi.yaml` с уже реализованными и запланированными endpoint-ами
- PostgreSQL migrations, создание служебного workspace `base`
- локальная идемпотентная команда `bootstrap-superadmin`
- CI, контейнерная сборка и базовая проектная инфраструктура

Product endpoint-ы этапа identity/workspace пока реализованы не полностью: регистрация, login/logout, авторизация и workspace-операции еще в работе.

## Требования

- Go 1.25 или новее

или

- Docker 24 или новее

Для локального lint OpenAPI дополнительно нужен Node.js 22+, но в обычной разработке это не обязательно, если проверка идет через CI.

## Быстрый локальный запуск

Запуск API без базы данных:

```text
go run ./cmd/filestore-api
```

Запуск CLI:

```text
go run ./cmd/filestore help
```

По умолчанию API слушает `:8080`. Приоритет конфигурации такой: флаги -> переменные окружения -> `.env` -> значения по умолчанию.

Примеры:

```text
go run ./cmd/filestore-api --listen=:8081
go run ./cmd/filestore config set api-url http://localhost:8081
go run ./cmd/filestore config get
```

Если база данных не настроена, API стартует только с инфраструктурными endpoint-ами и пишет предупреждение в лог. Это нормальное поведение для текущего этапа.

## PostgreSQL и bootstrap суперадминистратора

Поднять локальный PostgreSQL:

```text
docker compose up -d postgres
```

Настроить `FILESTORE_DATABASE_URL` можно по примеру из [.env.example](/C:/Users/alexonderia/Work/FileStore/.env.example). Для локальной разработки достаточно создать `.env` в корне проекта: API и CLI подхватывают его автоматически, если переменные еще не заданы в окружении. При старте API автоматически применяет встроенные миграции.

Создание или повышение первого суперадминистратора выполняется только локальной командой, пароль передается через stdin:

```text
echo "<SUPERADMIN_PASSWORD>" | go run ./cmd/filestore-api bootstrap-superadmin --name "System Admin" --email admin@example.test --password-stdin
```

Свойства команды:

- работает идемпотентно по нормализованному email
- не открывает HTTP endpoint для bootstrap
- не хранит пароль в миграциях, исходниках или параметрах процесса

## Проверка проекта

Основные локальные проверки:

```text
gofmt -w .
go vet ./...
go test -race ./...
go build ./cmd/filestore-api ./cmd/filestore
npx --yes @redocly/cli@1.34.3 lint openapi/openapi.yaml
```

Если нужен контейнерный прогон:

```text
docker build --target api -t filestore-api:local .
docker build --target cli -t filestore-cli:local .
```

Подробные ручные сценарии приемки описаны в [docs/stages/00-contract.md](/C:/Users/alexonderia/Work/FileStore/docs/stages/00-contract.md) и [docs/stages/01-identity-workspace.md](/C:/Users/alexonderia/Work/FileStore/docs/stages/01-identity-workspace.md).
