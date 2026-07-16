# Этап 2: файлы и версии — приёмка

## Автоматическая проверка

```text
docker compose up -d postgres seaweedfs
$env:FILESTORE_TEST_DATABASE_URL="postgres://filestore:filestore-local@localhost:5432/filestore?sslmode=disable"
$env:FILESTORE_TEST_S3_ENDPOINT="http://localhost:8333"
go test -count=1 -run TestFilesJourney ./tests/integration
```

Тест проверяет upload в SeaweedFS, атомарное создание файла и версии 1, SHA-256/размер, byte-for-byte download, history, encoding metadata, запрет чтения чужого base-файла и удаление S3-объекта при DB conflict.

## Ручной smoke-test

После регистрации и выбора workspace:

```text
filestore upload --name hello.txt ./hello.txt
filestore file list
filestore file info <FILE_ID>
filestore file history <FILE_ID>
filestore file download <FILE_ID> ./downloaded.txt
filestore file encoding set <FILE_ID> windows-1251
```

Сравнить исходный и скачанный файл побайтно. Повторное имя без учёта регистра должно вернуть `409` и не оставить объект в bucket.

## Готовность

- [x] SeaweedFS image закреплён версией 4.29.
- [x] Миграция `000003` и same-file FK применяются повторяемо.
- [x] Upload/download/history/encoding доступны через API, Go client и CLI.
- [x] Интеграционный сценарий проходит на PostgreSQL и SeaweedFS.
