# Этап 1: PostgreSQL, identity и workspace — ручная проверка

## Цель и границы

Проверить чистую установку PostgreSQL schema, идемпотентный `base`, bootstrap superadmin, регистрацию/login/logout, разграничение ролей и lifecycle private workspace. Документ является приёмочной инструкцией всего этапа; пункты выполняются после реализации соответствующих API/CLI-команд.

## Предусловия

- Успешно пройден `docs/stages/00-contract.md`.
- Docker и Docker Compose.
- Свободный порт PostgreSQL 5432 и API: либо default `8080`, либо тот же тестовый порт, на котором был завершён `docs/stages/00-contract.md` (например, `18081`).
- Временные значения `<ADMIN_PASSWORD>`, `<USER_PASSWORD>` длиной не менее 12 символов; реальные secrets в документ не записываются.

## Запуск и автоматические тесты

```text
docker compose up -d postgres
go test -race ./...
FILESTORE_TEST_DATABASE_URL=postgres://filestore:filestore-local@localhost:5432/filestore?sslmode=disable go test -count=1 ./tests/integration ./tests/e2e
go run ./cmd/filestore-api
```

Перед запуском API задать `FILESTORE_DATABASE_URL` по `.env.example`. Если вы продолжаете сценарий сразу после `docs/stages/00-contract.md` и API уже поднят на `:18081`, можно не перезапускать сервер, а использовать тот же адрес. Ожидается применение `000001` и `000002`, после чего повторный запуск не меняет количество migrations и base workspaces.

## Bootstrap superadmin

1. Передать `<ADMIN_PASSWORD>` через stdin в локальную команду:

   ```text
   echo "admin-password-123" | go run ./cmd/filestore-api bootstrap-superadmin --name "System Admin" --email admin@example.test --password-stdin
   ```

2. Повторить команду с тем же email.
3. Ожидается одна строка user с `is_superadmin=true`; ID не меняется, пароль/его hash не выводятся.
4. Вызов без `--password-stdin`, с коротким паролем или без database URL должен завершиться ошибкой до создания пользователя.

## Регистрация и вход

1. Зарегистрировать обычного пользователя через CLI и убедиться, что `is_superadmin=false` независимо от порядка регистрации:

   ```text
   go run ./cmd/filestore config set api-url http://localhost:8080
   echo "<USER_PASSWORD>" | go run ./cmd/filestore register --name "Owner" --email owner@example.test --password-stdin
   go run ./cmd/filestore auth me
   ```
   Если API продолжает слушать `:18081` после `docs/stages/00-contract.md`, вместо `http://localhost:8080` использовать `http://localhost:18081`.
2. Повторная регистрация email в другом регистре должна вернуть conflict и не создать дубль.
3. Войти правильным паролем; `auth me` должен вернуть того же user ID.
4. Проверить отказ с неправильным паролем и отсутствие password/token в логах.
5. Выполнить logout; повторное использование отозванного token должно вернуть 401.

## Workspace и роли

1. Получить зарезервированный `base`: он существует ровно один, не переименовывается и не удаляется.
2. Обычным пользователем создать private workspace командой `filestore workspace create <NAME>`. Создатель должен стать owner в той же транзакции.
3. Добавить editor и viewer командой `filestore workspace member add <WORKSPACE_ID> <EMAIL> <ROLE>`, проверить чтение workspace каждым участником.
4. Viewer не может изменять membership; editor также не управляет membership; owner может.
5. Попытка удалить последнего owner возвращает conflict.
6. Superadmin видит workspace без membership и может управлять пользователями/membership; событие фиксируется структурным security log.
7. Пользователь без membership получает скрытый not-found response и не видит метаданные private workspace.

## Конкурентные и негативные проверки

- Два конкурентных запроса регистрации одного email создают ровно одного user.
- Два конкурентных create workspace с одинаковым именем в разном регистре дают один success и один conflict.
- Повтор bootstrap во время обычной регистрации того же email не создаёт дубль и в итоге оставляет ровно одного superadmin.
- Одновременное удаление owners не может оставить private workspace без owner.
- Некорректный/истёкший/отозванный Bearer token всегда возвращает 401 в едином problem format.

## Очистка

```text
docker compose down -v
```

Удалить временный CLI config и очистить auth variables. Не копировать database volume или tokens в репозиторий.

## Итоговый checklist

- [ ] Миграции с нуля и повторный startup успешны.
- [ ] Существует ровно один `base`.
- [ ] Bootstrap superadmin идемпотентен и не раскрывает password/hash.
- [ ] Первый публичный registrant не получает superadmin.
- [ ] Register/login/me/logout работают, token revoke немедленный.
- [ ] Private workspace и owner создаются атомарно.
- [ ] Owner/editor/viewer и superadmin соответствуют матрице прав.
- [ ] Последний owner защищён при обычной и конкурентной операции.
- [ ] Case-insensitive ограничения email/workspace работают.
- [ ] Секреты отсутствуют в логах и локальных артефактах репозитория.

## Результат проверки

- Дата:
- Проверил:
- Commit:
- Окружение:
- Результат: пройдено / не пройдено
- Замечания:
