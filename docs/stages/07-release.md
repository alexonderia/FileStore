# Этап 7: минимальная release-проверка

Эта проверка подтверждает тестируемый MVP. Она не заменяет production backup policy, нагрузочные тесты и наблюдаемость.

## Clean install и verification

```text
Copy-Item .env.example .env
docker compose down -v
docker compose up -d postgres seaweedfs
go test ./...
$env:FILESTORE_TEST_DATABASE_URL="postgres://filestore:filestore-local@localhost:5432/filestore?sslmode=disable"
$env:FILESTORE_TEST_S3_ENDPOINT="http://localhost:8333"
go test -count=1 ./tests/integration ./tests/e2e
go vet ./...
go build ./cmd/filestore-api ./cmd/filestore
npx --yes @redocly/cli@1.34.3 lint openapi/openapi.yaml
```

Для race-проверки на Windows без CGO:

```text
docker run --rm -v "${PWD}:/src" -w /src golang:1.25-bookworm go test -race ./...
```

## Backup/restore minimum

1. Остановить API writes.
2. Выполнить `pg_dump -Fc` базы и snapshot volume `filestore-seaweed` под одним backup ID.
3. Восстановить оба артефакта в отдельный контур.
4. Запустить API, проверить migrations и скачать current/version link каждого контрольного файла с совпадающим SHA-256.
5. Не считать независимый DB dump без согласованного object-volume snapshot полным backup.

## Release checklist

- [x] PostgreSQL 16 и SeaweedFS 4.29 закреплены в Compose.
- [x] Миграции с нуля и повторное применение проверены.
- [x] Unit, integration, HTTP client journey, CLI e2e и container builds доступны.
- [x] OpenAPI отражает реализованные endpoints.
- [x] Пароли читаются из stdin; link tokens и raw bearer tokens не логируются.
- [ ] Перед production: выполнить restore drill на целевой инфраструктуре, добавить метрики/alerts и нагрузочный профиль.
