# FileStore

FileStore — минимально рабочий API-first сервис хранения файлов с версионностью, рабочими пространствами, update-сессиями, hard lock и автоматически создаваемыми ссылками. HTTP API, Go client и CLI работают с PostgreSQL и S3-совместимым SeaweedFS.

Короткая точка входа в документацию:

- Общее описание проекта и текущего устройства: [docs/PROJECT_OVERVIEW.md](/C:/Users/alexonderia/Work/FileStore/docs/PROJECT_OVERVIEW.md)
- Технический план MVP: [docs/TECHNICAL_DESIGN_MVP.md](/C:/Users/alexonderia/Work/FileStore/docs/TECHNICAL_DESIGN_MVP.md)
- Актуальная ER-схема: [docs/db_schemas/README.md](/C:/Users/alexonderia/Work/FileStore/docs/db_schemas/README.md)
- Ручная проверка этапа 0: [docs/stages/00-contract.md](/C:/Users/alexonderia/Work/FileStore/docs/stages/00-contract.md)
- Ручная проверка этапа 1: [docs/stages/01-identity-workspace.md](/C:/Users/alexonderia/Work/FileStore/docs/stages/01-identity-workspace.md)
- Приёмка файлов, update lifecycle, locks, links и release: [docs/stages](/C:/Users/alexonderia/Work/FileStore/docs/stages)

## Что уже работает

- HTTP API с `health/live` и `health/ready`
- CLI с identity/workspace, upload/download/history, update/diff/resolve/reject, lock/unlock и link-командами
- единый формат ошибок `application/problem+json`
- реализованный контракт `openapi/openapi.yaml`
- PostgreSQL migrations, создание служебного workspace `base`
- локальная идемпотентная команда `bootstrap-superadmin`
- регистрация/login/logout, hash Bearer tokens и немедленный revoke
- private workspace с транзакционным owner, ролями и защитой последнего owner
- централизованная permission policy и superadmin override с security events
- immutable objects и версии файлов в SeaweedFS, encoding-aware bounded diff и фоновые cleanup
- automatic current/version links: публичные для base и с проверкой membership для private workspace
- CI, контейнерная сборка и базовая проектная инфраструктура

## Требования

- Go 1.25 или новее

или

- Docker 24 или новее

Для локального lint OpenAPI дополнительно нужен Node.js 22+, но в обычной разработке это не обязательно, если проверка идет через CI.

## Быстрый локальный запуск

Запуск локальной инфраструктуры и API:

```text
Copy-Item .env.example .env
docker compose up -d postgres pgadmin seaweedfs
go run ./cmd/filestore-api
```

Запуск CLI:

```text
go run ./cmd/filestore help
```

По умолчанию API слушает `:8080`. Приоритет конфигурации такой: флаги -> переменные окружения -> `.env` -> значения по умолчанию.

Локальная PostgreSQL admin UI доступна через pgAdmin:

- URL: `http://localhost:5050`
- login: значение `PGADMIN_DEFAULT_EMAIL` из `.env`
- password: значение `PGADMIN_DEFAULT_PASSWORD` из `.env`
- host для подключения к БД внутри pgAdmin: `postgres`
- port: `5432`
- database/user/password: из `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`

Примеры:

```text
go run ./cmd/filestore-api --listen=:8081
go run ./cmd/filestore config set api-url http://localhost:8081
go run ./cmd/filestore config get
```

Если база данных или object storage не настроены, API стартует с доступной частью endpoint-ов и пишет предупреждение в лог.

После запуска с PostgreSQL можно пройти базовый CLI-сценарий (пароль не передаётся аргументом процесса):

```text
echo "<USER_PASSWORD>" | go run ./cmd/filestore register --name "Owner" --email owner@example.test --password-stdin
go run ./cmd/filestore auth me
go run ./cmd/filestore workspace show-base
go run ./cmd/filestore workspace use 00000000-0000-0000-0000-000000000001
go run ./cmd/filestore upload ./example.txt
go run ./cmd/filestore file list
```

## PostgreSQL и bootstrap суперадминистратора

Поднять локальный PostgreSQL:

```text
docker compose up -d postgres pgadmin
```

Настроить `FILESTORE_DATABASE_URL` можно по примеру из [.env.example](/C:/Users/alexonderia/Work/FileStore/.env.example). Для локальной разработки достаточно создать `.env` в корне проекта: API и CLI подхватывают его автоматически, если переменные еще не заданы в окружении. При старте API автоматически применяет встроенные миграции.

Контрактные defaults MVP: upload 100 MiB, update-session TTL 24 часа, diff 1 MiB и 20 000 строк на сторону с output до 1 MiB, orphan grace period 24 часа; кодировки `utf-8`, `utf-16le`, `utf-16be`, `windows-1251`. Все значения отражены в `.env.example` и ADR 0004.

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
go fmt ./...
go vet ./...
go test -race ./...
go build ./cmd/filestore-api ./cmd/filestore
npx --yes @redocly/cli@1.34.3 lint openapi/openapi.yaml
```

Интеграционные и CLI e2e-тесты с реальным PostgreSQL включаются явно:

```text
$env:FILESTORE_TEST_DATABASE_URL="postgres://filestore:filestore-local@localhost:5432/filestore?sslmode=disable"
$env:FILESTORE_TEST_S3_ENDPOINT="http://localhost:8333"
go test -count=1 ./tests/integration ./tests/e2e
```

Если локальный `go test -race ./...` на Windows завершается ошибкой вида `-race requires cgo`, это ограничение окружения, а не проекта. В таком случае локально достаточно выполнить:

```text
go test ./...
```

А `-race` оставить обязательной проверкой для CI или окружения с включенным `cgo`.

Эквивалентный Linux-прогон на Windows:

```text
docker run --rm -v "${PWD}:/src" -w /src golang:1.25-bookworm go test -race ./...
```

Если нужен контейнерный прогон:

```text
docker build --target api -t filestore-api:local .
docker build --target cli -t filestore-cli:local .
```

Подробные ручные сценарии приемки описаны в [docs/stages](/C:/Users/alexonderia/Work/FileStore/docs/stages).
