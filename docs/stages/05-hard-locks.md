# Этап 5: hard lock — приёмка

## Ручной smoke-test

```text
filestore file lock <FILE_ID>
filestore file lock-status <FILE_ID>
filestore file encoding set <FILE_ID> utf-8
filestore file unlock <FILE_ID>
```

Под lock чтение, download и history остаются доступны, а write-операции отвечают `423`. Active update session и hard lock не могут существовать одновременно: создание lock при session даёт `409`, создание session при lock — `423`.

## Готовность

- [x] Один active lock обеспечен partial unique index.
- [x] Создание lock/session сериализовано через строку `files`.
- [x] Encoding update, resolve и link revoke используют write guard.
- [x] Unlock доступен creator, locker, editor/owner и superadmin; release сохраняется в истории БД.
