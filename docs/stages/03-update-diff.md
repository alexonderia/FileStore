# Этап 3: update session и diff — приёмка

## Автоматическая проверка

```text
go test -count=1 -run TestMVPUpdateLockAndLinkJourney ./tests/integration
```

Сценарий проверяет загрузку candidate, повтор с тем же `Idempotency-Key`, неизменность current version до resolve и bounded unified diff.

## Ручной smoke-test

```text
filestore update create --key manual-update-0001 <FILE_ID> ./candidate.txt
filestore update diff <FILE_ID> <SESSION_ID>
```

Повтор первой команды с тем же ключом возвращает ту же session. Для binary/слишком больших/некорректно декодируемых данных ответ должен быть `metadata_only`, а не неограниченный diff.

## Готовность

- [x] На файл допускается одна active session.
- [x] Candidate хранится отдельно от published versions.
- [x] Diff учитывает `utf-8`, UTF-16 и Windows-1251 и ограничен конфигурацией.
- [x] SHA старой версии формирует rollback warning.
