# Этап 6: ссылки — приёмка

## Ручной smoke-test

```text
filestore file links <FILE_ID>
filestore link download <CURRENT_TOKEN> ./current.bin
filestore link download <VERSION_TOKEN> ./version.bin
filestore link revoke <FILE_ID> <LINK_ID>
```

При создании файла автоматически появляются current-link и version-link v1; каждый resolve добавляет version-link. Current-link следует за текущей версией, version-link остаётся неизменным. Base links доступны анонимно; private links требуют Bearer token участника. Отозванный и неизвестный token одинаково возвращают `404`.

## Готовность

- [x] Миграция `000006` создаёт constraints и идемпотентный backfill.
- [x] Создание link входит в транзакцию публикации файла/версии.
- [x] Token имеет 256 бит entropy, 43 base64url символа и не попадает в server logs.
- [x] Revoke необратим и блокируется hard lock.
