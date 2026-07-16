# ADR 0004: контрактные лимиты и capability tokens MVP

- Статус: принято
- Дата: 2026-07-16

## Контекст

Решения A-01–A-21 задают общий shape MVP, но несколько из них оставляли deployment-значения открытыми: hard upload limit, update-session TTL, границы diff, allowlist кодировок, orphan grace period и способ хранения link token. Без единых defaults OpenAPI, конфигурация, миграции и тесты могли реализовать несовместимое поведение.

## Решение

### Стек и границы процесса

- Go 1.25 является минимальной версией языка; используются `net/http`, стандартный `flag`, `pgx/v5` и AWS SDK for Go v2 для будущего S3-compatible adapter.
- API и CLI остаются двумя бинарниками одного Go module. CLI обращается только к HTTP API.
- PostgreSQL является источником истины метаданных; SeaweedFS хранит immutable bytes. Распределённая транзакция не моделируется.
- Range download не входит в MVP-контракт. До явного добавления ответа `206` API отдаёт только полный объект.
- Standalone user-administration API откладывается: в MVP superadmin создаётся/обновляется deployment-командой, получает workspace/file override и управляет membership. Password reset, disable/delete user и выдача superadmin через публичный API требуют отдельной модели lifecycle и аудита.

### Серверные defaults

| Параметр | Default | Переменная окружения |
|---|---:|---|
| Максимальный размер одного upload | 100 MiB (`104857600` bytes) | `FILESTORE_MAX_FILE_SIZE` |
| TTL active update session | 24 часа | `FILESTORE_UPDATE_SESSION_TTL` |
| Максимальный декодированный input diff на одну сторону | 1 MiB (`1048576` bytes) | `FILESTORE_DIFF_MAX_INPUT_BYTES` |
| Максимум строк на одну сторону diff | 20 000 | `FILESTORE_DIFF_MAX_LINES` |
| Максимальный unified diff | 1 MiB (`1048576` bytes) | `FILESTORE_DIFF_MAX_OUTPUT_BYTES` |
| Минимальный возраст orphan до удаления | 24 часа | `FILESTORE_ORPHAN_GRACE_PERIOD` |

Все значения являются положительными hard limits и валидируются до запуска listener. Upload обрабатывается потоково и прекращается сразу после превышения лимита. Server clock является единственным источником времени session/cleanup.

### Text encoding и diff

- Canonical allowlist: `utf-8`, `utf-16le`, `utf-16be`, `windows-1251`.
- `utf-8` обязателен и используется по умолчанию. Deployment может сузить allowlist через `FILESTORE_TEXT_ENCODINGS`, но не может добавить decoder без изменения кода и тестов.
- Diff строится построчно в Unicode, только когда обе стороны успешно декодированы и не превышают каждый input/line limit.
- Binary detection, ошибка декодирования либо превышение любого input/output limit возвращают `metadata_only` с причиной; частичный unified diff не возвращается.
- Metadata comparison всегда содержит original name, MIME, byte size и SHA-256 обеих сторон. `rollback_warning=true`, только если candidate hash совпадает с опубликованной не-current версией того же файла.

### Link capability token

- Link token содержит 256 случайных бит и кодируется как 43-символьный base64url без padding.
- В MVP `file_links.token` хранит token в plaintext. Это осознанный компромисс: автоматически созданные links должны повторно отображаться авторизованному creator/editor/owner, а reissue endpoint и отдельное управление encryption key исключены из MVP.
- Link token считается секретом: он не попадает в structured logs, problem responses, security events, метрики или обычный вывод диагностики. Access logs обязаны редактировать path `/links/{token}`.
- PostgreSQL backup считается secret-bearing артефактом и защищается как credentials. Переход на envelope encryption или hash + explicit reissue оформляется отдельной миграцией после MVP.
- Для недействительного, отозванного и неизвестного публичного token возвращается одинаковый `404`.

### Идемпотентность и cleanup

- `Idempotency-Key` обязателен для create-file, create-session и resolve; scope включает actor и operation target.
- Повтор с тем же key и тем же payload возвращает сохранённый результат; другой payload возвращает conflict.
- Cleanup сначала фиксирует terminal state в PostgreSQL, затем удаляет bytes. Orphan моложе grace period не удаляется.

## Последствия

- Defaults входят одновременно в API config, `.env.example`, OpenAPI descriptions и тесты.
- D-03/D-04 не выбирают собственные значения лимитов и не расширяют encoding allowlist без нового ADR.
- Plaintext link tokens повышают требования к DB backup и логированию, но сохраняют заявленную операцию повторного просмотра автоматически созданных links.
- Решения A-01–A-21 считаются принятыми; standalone user lifecycle и range requests явно отложены и не блокируют MVP.
