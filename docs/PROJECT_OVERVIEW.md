# FileStore: описание проекта

## Назначение

FileStore — это сервис хранения файлов с версионностью и контролируемым совместным доступом. Идея проекта в том, чтобы файл был не просто набором байт, а управляемой сущностью с историей версий, правами доступа, рабочими пространствами, блокировками и ссылками для чтения текущей или конкретной версии.

Проект строится по модели API-first:

- HTTP API является главным контрактом системы
- CLI работает только через API
- OpenAPI фиксирует реализованные endpoint-ы MVP

## Из чего состоит проект сейчас

### 1. API

Серверный процесс запускается из `cmd/filestore-api`.

На текущем этапе он умеет:

- поднимать HTTP-сервер
- отдавать `GET /health/live`
- отдавать `GET /health/ready`
- возвращать ошибки в формате `application/problem+json`
- читать конфигурацию из флагов и переменных окружения
- подключаться к PostgreSQL при наличии `FILESTORE_DATABASE_URL`
- автоматически применять встроенные миграции
- регистрировать и аутентифицировать пользователей, отзывать Bearer token
- создавать/list/read workspace и управлять membership с проверкой ролей
- загружать, скачивать и версионировать файлы через SeaweedFS
- создавать update session, показывать bounded diff и выполнять resolve/reject
- управлять hard lock и автоматически созданными current/version links

Если база данных не задана, API все равно запускается, но продуктовые endpoint-ы недоступны. Это сделано специально, чтобы можно было отдельно проверять каркас приложения и инфраструктурные части.

### 2. CLI

Клиентский процесс запускается из `cmd/filestore`.

Сейчас доступны команды:

- `help`
- `version`
- `config get`
- `config set`
- `register`, `login`, `logout`, `auth me`
- `workspace show-base`, `workspace list`, `workspace create`, `workspace use`
- `workspace member add`, `workspace member remove`
- `upload`, `file list/info/history/download/encoding`
- `update create/diff/resolve/reject`
- `file lock/lock-status/unlock/links`, `link revoke/download`

CLI хранит локальную конфигурацию пользователя: адрес API, выбранный workspace и полученный токен в файле с ограниченными правами. Пароль читается только из stdin, а сырой token не печатается командами входа и регистрации.

### 3. OpenAPI-контракт

Файл [openapi/openapi.yaml](/C:/Users/alexonderia/Work/FileStore/openapi/openapi.yaml) — реализованный технический контракт между API, CLI и тестами.

Это позволяет:

- заранее фиксировать форму запросов и ответов
- валидировать спецификацию независимо от готовности бизнес-логики
- развивать CLI и сервер вокруг одного контракта

### 4. База данных и миграции

PostgreSQL используется как основной источник истины для:

- пользователей
- рабочих пространств
- membership и ролей
- токенов доступа
- файлов, immutable versions, блокировок, ссылок и сессий обновления

Миграции лежат в каталоге `migrations` и встраиваются в бинарник API. При старте сервера они применяются автоматически.

Уже реализованы:

- таблицы пользователей
- таблицы workspace
- таблицы membership
- таблицы токенов пользователя
- создание системного workspace `base`
- storage objects, files/versions, update sessions, locks и links

### 5. Bootstrap суперадминистратора

Первый суперадминистратор создается не через HTTP, а через локальную команду:

```text
echo "<SUPERADMIN_PASSWORD>" | go run ./cmd/filestore-api bootstrap-superadmin --name "System Admin" --email admin@example.test --password-stdin
```

Это решение принято специально, чтобы:

- случайный первый зарегистрированный пользователь не получал права суперадмина
- не появлялся опасный публичный bootstrap endpoint
- создание первого администратора было контролируемой операцией развертывания

Команда идемпотентная: повторный запуск с тем же email не создает дубль.

### 6. Документация этапов

В каталоге `docs/stages` лежат файлы ручной приемки. Это не исполняемые скрипты, а именно человекочитаемые инструкции, по которым можно проверить, что очередной этап действительно работает.

Сейчас есть:

- [docs/stages/00-contract.md](/C:/Users/alexonderia/Work/FileStore/docs/stages/00-contract.md) — проверка каркаса, контракта и базового запуска
- [docs/stages/01-identity-workspace.md](/C:/Users/alexonderia/Work/FileStore/docs/stages/01-identity-workspace.md) — приемка этапа identity/workspace
- `02-files-versions.md`–`06-links.md` — приёмка продуктовой вертикали
- `07-release.md` — clean-install и минимальный release checklist

## Текущий статус реализации

На 16 июля 2026 года собрана минимально рабочая версия D-00–D-08:

- инфраструктурный каркас уже собран
- API и CLI компилируются
- CI и контейнерная сборка настроены
- PostgreSQL и миграции подключены
- bootstrap суперадминистратора работает
- register/login/logout и token validation работают
- workspace API/CLI, роли и last-owner guard работают
- PostgreSQL/SeaweedFS integration и HTTP Go-client journey покрывают upload, update, resolve, lock и links
- API/CLI предоставляют полную минимальную продуктовую вертикаль

Перед production остаются инфраструктурный restore drill, метрики/alerts, нагрузочные и расширенные fault-injection проверки. Они отмечены отдельно в `docs/stages/07-release.md` и не мешают локальному тестированию MVP.

## Как проект запускается сейчас

### Вариант 1. Только API и CLI без базы

Подходит для проверки каркаса приложения.

```text
go run ./cmd/filestore-api
go run ./cmd/filestore help
```

Что будет доступно:

- health endpoint-ы
- базовая конфигурация CLI
- единый формат ошибок

Что будет недоступно:

- все product endpoint-ы, которым нужна бизнес-логика и база данных

### Вариант 2. API с PostgreSQL

1. Поднять PostgreSQL:

```text
docker compose up -d postgres
```

2. Задать строку подключения:

```text
FILESTORE_DATABASE_URL=postgres://filestore:filestore-local@localhost:5432/filestore?sslmode=disable
```

3. Запустить API:

```text
go run ./cmd/filestore-api
```

При старте сервер:

- открывает соединение с PostgreSQL
- проверяет доступность базы
- применяет встроенные миграции
- запускает HTTP API

### Вариант 3. Создание первого суперадминистратора

После старта PostgreSQL можно отдельно выполнить:

```text
echo "<SUPERADMIN_PASSWORD>" | go run ./cmd/filestore-api bootstrap-superadmin --name "System Admin" --email admin@example.test --password-stdin
```

Эта команда:

- валидирует входные данные
- хеширует пароль
- создает пользователя или повышает существующего
- не печатает пароль или его hash

## Конфигурация

Основные параметры сейчас такие:

- `FILESTORE_API_LISTEN` — адрес HTTP listener, по умолчанию `:8080`
- `FILESTORE_API_READ_HEADER_TIMEOUT` — timeout чтения заголовков
- `FILESTORE_API_SHUTDOWN_TIMEOUT` — graceful shutdown timeout
- `FILESTORE_AUTH_TOKEN_TTL` — срок жизни Bearer token, по умолчанию `24h`
- `FILESTORE_MAX_FILE_SIZE` — hard upload limit, по умолчанию 100 MiB
- `FILESTORE_UPDATE_SESSION_TTL` — TTL update session, по умолчанию `24h`
- `FILESTORE_DIFF_MAX_INPUT_BYTES` — decoded bytes на сторону diff, по умолчанию 1 MiB
- `FILESTORE_DIFF_MAX_LINES` — строки на сторону diff, по умолчанию 20 000
- `FILESTORE_DIFF_MAX_OUTPUT_BYTES` — максимальный unified diff, по умолчанию 1 MiB
- `FILESTORE_ORPHAN_GRACE_PERIOD` — минимальный возраст orphan, по умолчанию `24h`
- `FILESTORE_TEXT_ENCODINGS` — allowlist; default `utf-8,utf-16le,utf-16be,windows-1251`
- `FILESTORE_DATABASE_URL` — строка подключения к PostgreSQL
- `FILESTORE_CONFIG` — путь к локальному конфигу CLI

Для локального PostgreSQL используются также:

- `POSTGRES_USER`
- `POSTGRES_PASSWORD`
- `POSTGRES_DB`
- `POSTGRES_PORT`

Пример значений есть в [.env.example](/C:/Users/alexonderia/Work/FileStore/.env.example).

Для локальной разработки API и CLI автоматически читают `.env` из корня репозитория. Если нужная переменная уже задана в окружении Windows или PowerShell, она имеет приоритет над значением из `.env`.

## Как проверять проект

Автоматические проверки:

```text
go fmt ./...
go vet ./...
go test -race ./...
go build ./cmd/filestore-api ./cmd/filestore
npx --yes @redocly/cli@1.34.3 lint openapi/openapi.yaml
```

Проверки с реальным PostgreSQL:

```text
FILESTORE_TEST_DATABASE_URL=postgres://filestore:filestore-local@localhost:5432/filestore?sslmode=disable go test -count=1 ./tests/integration ./tests/e2e
```

Если локально на Windows `go test -race ./...` недоступен из-за отсутствия `cgo`, рабочим минимумом считается `go test ./...`, а проверка `-race` переносится в CI или в отдельное окружение с настроенным C toolchain.

Проверку можно локально выполнить в Linux-контейнере: `docker run --rm -v "${PWD}:/src" -w /src golang:1.25-bookworm go test -race ./...`.

Ручная приемка:

- пройти [docs/stages/00-contract.md](/C:/Users/alexonderia/Work/FileStore/docs/stages/00-contract.md)
- затем пройти [docs/stages/01-identity-workspace.md](/C:/Users/alexonderia/Work/FileStore/docs/stages/01-identity-workspace.md) по мере завершения этапа

## Куда проект будет развиваться дальше

Следующие крупные блоки реализации:

- файловое хранилище и версии
- изменение кодировки файла как метаданных
- update sessions и diff
- hard lock
- автоматические current/version links

Подробный пошаговый план уже зафиксирован в [docs/TECHNICAL_DESIGN_MVP.md](/C:/Users/alexonderia/Work/FileStore/docs/TECHNICAL_DESIGN_MVP.md).
