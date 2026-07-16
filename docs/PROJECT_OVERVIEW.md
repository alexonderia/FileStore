# FileStore: описание проекта

## Назначение

FileStore — это сервис хранения файлов с версионностью и контролируемым совместным доступом. Идея проекта в том, чтобы файл был не просто набором байт, а управляемой сущностью с историей версий, правами доступа, рабочими пространствами, блокировками и ссылками для чтения текущей или конкретной версии.

Проект строится по модели API-first:

- HTTP API является главным контрактом системы
- CLI работает только через API
- OpenAPI фиксирует текущие и будущие endpoint-ы

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

Если база данных не задана, API все равно запускается, но продуктовые endpoint-ы недоступны. Это сделано специально, чтобы можно было отдельно проверять каркас приложения и инфраструктурные части.

### 2. CLI

Клиентский процесс запускается из `cmd/filestore`.

Сейчас доступны команды:

- `help`
- `version`
- `config get`
- `config set`

CLI хранит локальную конфигурацию пользователя: адрес API и выбранный workspace. Дальше именно через этот CLI будут добавляться команды регистрации, входа, управления workspace и файлами.

### 3. OpenAPI-контракт

Файл [openapi/openapi.yaml](/C:/Users/alexonderia/Work/FileStore/openapi/openapi.yaml) — это технический контракт между API, CLI и будущими тестами. В нем уже описаны продуктовые операции, даже если часть из них еще не реализована в коде.

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
- в дальнейшем версий, блокировок, ссылок и сессий обновления

Миграции лежат в каталоге `migrations` и встраиваются в бинарник API. При старте сервера они применяются автоматически.

Уже реализованы:

- таблицы пользователей
- таблицы workspace
- таблицы membership
- таблицы токенов пользователя
- создание системного workspace `base`

### 5. Bootstrap суперадминистратора

Первый суперадминистратор создается не через HTTP, а через локальную команду:

```text
go run ./cmd/filestore-api bootstrap-superadmin --name "System Admin" --email admin@example.test --password-stdin
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

## Текущий статус реализации

На 16 июля 2026 года проект находится между этапами D-01 и D-02:

- инфраструктурный каркас уже собран
- API и CLI компилируются
- CI и контейнерная сборка настроены
- PostgreSQL и миграции подключены
- bootstrap суперадминистратора работает

Еще не завершены:

- регистрация пользователя
- login/logout
- проверка Bearer token в API
- продуктовые endpoint-ы workspace
- соответствующие CLI-команды

То есть проект уже можно запускать, собирать, тестировать и использовать как технический каркас, но бизнес-функции хранения файлов и управления доступом еще не доведены до рабочего состояния.

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
FILESTORE_DATABASE_URL=postgres://filestore:change-me@localhost:5432/filestore?sslmode=disable
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
gofmt -w .
go vet ./...
go test -race ./...
go build ./cmd/filestore-api ./cmd/filestore
npx --yes @redocly/cli@1.34.3 lint openapi/openapi.yaml
```

Ручная приемка:

- пройти [docs/stages/00-contract.md](/C:/Users/alexonderia/Work/FileStore/docs/stages/00-contract.md)
- затем пройти [docs/stages/01-identity-workspace.md](/C:/Users/alexonderia/Work/FileStore/docs/stages/01-identity-workspace.md) по мере завершения этапа

## Куда проект будет развиваться дальше

Следующие крупные блоки реализации:

- identity API и CLI
- workspace API и CLI
- файловое хранилище и версии
- изменение кодировки файла как метаданных
- update sessions и diff
- hard lock
- автоматические current/version links

Подробный пошаговый план уже зафиксирован в [docs/TECHNICAL_DESIGN_MVP.md](/C:/Users/alexonderia/Work/FileStore/docs/TECHNICAL_DESIGN_MVP.md).
