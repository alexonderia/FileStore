# Этап 4: lifecycle обновления — приёмка

## Проверка

```text
filestore update resolve <FILE_ID> <SESSION_ID>
filestore file history <FILE_ID>
filestore update create --key manual-reject-0001 <FILE_ID> ./candidate.txt
filestore update reject <FILE_ID> <SESSION_ID>
```

Resolve атомарно создаёт следующую published version и повторно возвращает её же. Reject не меняет current version. Встроенный scheduler раз в минуту завершает просроченные sessions; orphan reconciliation удаляет неиспользуемую DB metadata и соответствующий S3 object после grace period.

## Готовность

- [x] State machine имеет `active/resolved/rejected/expired`.
- [x] Resolve проверяет base=current под row lock.
- [x] Resolve идемпотентен, version number уникален.
- [x] Reject/expire отделяют terminal transition от повторяемой очистки объекта.
