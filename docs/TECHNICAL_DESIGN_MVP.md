# FileStore MVP — технический план реализации

Статус документа: утверждённый baseline MVP, активная реализация.
Основание: `FileStore_MVP.docx` (v0.9, 15.07.2026), `filestore_conflicts_summary.docx` (06.07.2026), `db_schemas/15-07-mvp.png`.

## 1. Общая оценка проекта

FileStore MVP имеет жизнеспособное и достаточно простое ядро: PostgreSQL хранит бизнес-состояние и связи, SeaweedFS — неизменяемые байты, API является единственной точкой авторизации и изменения состояния, CLI работает только через API. Отдельная опубликованная версия на каждый объект, указатель `files.current_version_id`, optimistic-проверка `base_version_id` и два разных вида блокировок образуют согласованную основу.

Контрактные неоднозначности исходных документов закрыты решениями A-01–A-21, ADR 0001–0004, OpenAPI и актуальной ER-схемой. Пользователь регистрируется или входит по учётным данным, первоначальный суперадминистратор создаётся отдельной bootstrap-командой, ссылки создаются автоматически для файла и каждой версии, а кодировка файла является изменяемой метаинформацией. Лимиты upload/diff, TTL, allowlist кодировок, хранение link token и согласованность PostgreSQL/SeaweedFS теперь имеют единый нормативный baseline.

### 1.1. Приоритет источников

1. `FileStore_MVP.docx` — нормативный документ MVP.
2. `15-07-mvp.png` — иллюстрация к основному документу. При расхождении приоритет имеет текст и DDL основного документа.
3. `filestore_conflicts_summary.docx` — аналитический документ и обоснование решений. Его устаревшие тезисы не заменяют актуальную модель MVP.

Это важно, потому что сводка по конфликтам говорит о `base_version_id` и `file_locks` как о будущем развитии, тогда как основной документ уже включает их в MVP.

### 1.2. Рекомендуемый технический контур

- Один репозиторий на Go: отдельные исполняемые приложения API и CLI.
- Один API-процесс; cleanup запускается внутри него как управляемая фоновая задача. Отдельный worker для MVP не нужен.
- PostgreSQL — единственный источник истины о доступе, версиях, сессиях, ссылках и блокировках.
- Один S3-compatible bucket SeaweedFS; объектный ключ генерирует сервер, перезапись объекта запрещена организацией ключей.
- OpenAPI — контракт между API и CLI.
- CLI не обращается напрямую ни к PostgreSQL, ни к SeaweedFS.
- Распределённой транзакции нет; согласованность PostgreSQL и SeaweedFS достигается порядком операций, компенсацией и периодической сверкой.

Go 1.25+ утверждён как технологический baseline ADR 0004. Смена языка или разделение API/CLI по разным репозиториям является архитектурным изменением и требует нового ADR.

## 2. Проверка архитектуры

### 2.1. Инварианты модели данных

| Область | Обязательный инвариант | Как обеспечить в MVP |
|---|---|---|
| Base workspace | Существует ровно один workspace с именем `base`; он не переименовывается и не удаляется | Идемпотентное создание миграцией/инициализацией, регистронезависимая уникальность имени и запрет мутаций в сервисе |
| Identity | Пользователь получает identity только после регистрации или входа; первоначальный superadmin создаётся явной bootstrap-командой | Password hash и session/token hash хранятся в БД; bootstrap идемпотентен и не назначает superadmin первому случайному registrant |
| Приватный workspace | Создатель сразу является `owner`; без membership доступ запрещён | Создание workspace и owner membership в одной транзакции |
| Логический файл | Имя уникально внутри workspace; `current_version_id` после завершения создания не пуст | Проверка/ограничение уникальности и атомарное создание файла с первой версией |
| Версия | Номер положительный и уникальный в пределах файла; опубликованная версия неизменяема | Ограничения БД, только операции создания и чтения |
| Current version | Указанная версия принадлежит тому же `file_id` | Составная ссылочная целостность либо обязательная транзакционная проверка; предпочтительно ограничение БД |
| Storage object | Ключ уникален и никогда не переиспользуется; опубликованный объект не удаляется | Серверный UUID-ключ и удаление только кандидатов/сирот |
| Update session | У файла не более одной `active`-сессии; base/resolved versions принадлежат файлу | Частичный уникальный индекс, блокировка строки файла и проверка принадлежности |
| Hard lock | У файла не более одной активной полной блокировки | Частичный уникальный индекс и сериализация через строку файла |
| Два вида блокировки | Активная update session и hard lock не существуют одновременно | Все операции их создания блокируют строку `files` и проверяют вторую сущность в той же транзакции |
| Link | У каждого файла автоматически существует одна current-ссылка, у каждой опубликованной версии — одна version-ссылка; версионная ссылка указывает на версию этого же файла | Создание ссылок в транзакции создания файла/версии, уникальные ограничения по типу и составная ссылочная целостность; revoke меняет статус, но не удаляет запись |
| Кодировка | Кодировка является изменяемой метаинформацией логического файла и не меняет сохранённые байты опубликованных версий | По умолчанию `utf-8`, при upload допустимо явное нормализованное IANA-имя из поддерживаемого allowlist; отдельная авторизованная операция смены под file row lock |
| Авторство | Автор берётся только из подтверждённой identity, не из `user.name/email` CLI | Authentication middleware передаёт `users.id` сервису |

Поля, связывающие две сущности одного файла (`current_version_id`, `base_version_id`, `resolved_version_id`, `file_links.version_id`), в текущем DDL допускают ошибочную связь с версией другого файла. Для надёжности следует добавить составные ограничения принадлежности. Если выбранный инструмент миграций затрудняет циклическую ссылку `files` ↔ `file_versions`, внешнее ограничение добавляется после создания обеих таблиц.

Статусные поля также должны образовывать непротиворечивые наборы: active session имеет candidate object и не имеет `completed_at/resolved_version_id`; resolved session имеет candidate object, resolved version и completion time; rejected/expired session не имеет candidate/resolved version и имеет completion time. У released lock одновременно заполнены `released_at` и `released_by_user_id`. Эти правила желательно закрепить простыми CHECK constraints и обязательно продублировать доменными тестами.

### 2.2. Жизненный цикл файла и storage object

#### Первая загрузка

1. API аутентифицирует пользователя, проверяет workspace, роль, имя и лимит размера.
2. API создаёт уникальный object key, потоково отправляет байты в SeaweedFS и одновременно считает SHA-256 и размер. MIME определяется сервером; неизвестный тип получает `application/octet-stream`.
3. После успешного `PutObject` одна транзакция PostgreSQL создаёт `storage_objects`, `files`, первую `file_versions`, автоматически создаёт current-link файла и version-link первой версии и выставляет `current_version_id`.
4. Если транзакция не зафиксировалась, API пытается удалить объект. Неудачная компенсация оставляет S3-only сироту, которую позже обнаруживает сверка bucket keys с `storage_objects`.
5. `files.current_version_id = NULL` допустим только внутри незавершённой транзакции и никогда не должен быть видим клиенту.

Предварительная проверка уникальности имени полезна, но не является гарантией: окончательное решение принимает ограничение БД внутри транзакции.

#### Создание update session

1. API предварительно проверяет доступ и наличие файла.
2. Кандидат загружается под новым ключом в SeaweedFS; вычисляются hash, size и MIME.
3. Транзакция блокирует строку файла, затем повторно проверяет hard lock, активную сессию и актуальную версию.
4. В той же транзакции создаются `storage_objects` и `file_update_sessions`; `base_version_id` фиксируется из текущего состояния под блокировкой.
5. При конфликте или ошибке объект компенсирующе удаляется. Уникальный индекс остаётся последней защитой от гонки.

DB-транзакцию нельзя держать открытой во время сетевой загрузки в SeaweedFS. Поэтому временная сирота при сбое является ожидаемым, контролируемым состоянием.

#### Diff и риск отката

Diff не хранится. При запросе API читает base и candidate objects и декодирует их согласно `files.text_encoding`. При неправильном отображении пользователь с правом изменения файла может сменить кодировку и повторить запрос diff; сохранённые байты и история версий при этом не меняются. API возвращает:

- для текстовых файлов с поддерживаемой кодировкой в пределах 1 MiB decoded bytes и 20 000 строк на сторону, если output не превышает 1 MiB, — построчную разницу в Unicode с `kind=text`;
- для бинарных, неизвестных, ошибочно декодируемых или слишком больших файлов — только сравнение имени, MIME, размера и SHA-256 с `kind=metadata_only` и точной причиной fallback;
- предупреждение о возможном откате, если SHA-256 кандидата совпал с одной из более ранних опубликованных версий того же файла, но не с current.

Автоматический merge отсутствует. Результат diff информационный и не меняет решение о публикации.

#### Resolve

1. Транзакция блокирует строку файла, затем строку сессии; такой порядок должен быть одинаковым во всех write-сценариях.
2. Проверяются авторизация, статус `active`, срок действия, отсутствие hard lock, принадлежность base/candidate и равенство `current_version_id == base_version_id`.
3. Следующий `version_number` определяется внутри этой транзакции.
4. Создаётся `file_versions` и автоматически создаётся его version-link, меняются `current_version_id` и `updated_at`, сессия переводится в `resolved` и получает `resolved_version_id/completed_at`.
5. Storage object не копируется и не удаляется: тот же неизменяемый объект становится опубликованным.

Повтор resolve после потерянного ответа должен быть идемпотентен: если сессия уже `resolved`, API возвращает ранее созданную версию, а не создаёт ещё одну.

#### Reject и expire

Сначала транзакция необратимо переводит активную сессию в `rejected` или `expired`, заполняет `completed_at` и отвязывает candidate object. После commit выполняется удаление из SeaweedFS. Строка `storage_objects` удаляется только после подтверждённого удаления объекта. При ошибке она остаётся сиротой для повторной очистки.

Такой порядок исключает публикацию уже отклонённого объекта и не теряет сведения, необходимые для повторной очистки. Resolve для истёкшей по времени сессии запрещён даже до прохода фоновой задачи; endpoint лениво переводит её в `expired` либо возвращает согласованную ошибку и инициирует cleanup.

Reconciliation обязана покрывать два разных случая: строку `storage_objects`, на которую больше никто не ссылается, и объект SeaweedFS, для которого DB-строка никогда не зафиксировалась. Второй случай обнаруживается только ограниченным листингом bucket/prefix и сравнением ключей; одного запроса к PostgreSQL недостаточно. В обоих случаях применяется grace period и повторная проверка перед удалением.

### 2.3. Versioning

- Версии линейны; граф родителей, merge parents и conflict versions в MVP отсутствует.
- `file_versions` содержит только опубликованные версии. Кандидат живёт только в update session.
- Новая версия никогда не перезаписывает байты предыдущей.
- Current всегда указывает на опубликованную версию того же файла.
- Rollback/change-current не входит в заявленные API и CLI. Упоминания запрета «смены текущей версии» под hard lock следует считать правилом для будущей операции, а не скрытой функцией MVP.
- Дедупликация по SHA-256 не выполняется: текущая уникальность `file_versions.storage_object_id` предполагает отдельный объект на каждую версию. Hash используется для целостности, сравнения и предупреждения о возврате к старому содержимому.

### 2.4. Workspace и права

Рекомендуемая матрица MVP:

| Операция | `base` | Приватный workspace |
|---|---|---|
| Анонимное чтение | Только по активной ссылке | Никогда |
| Аутентифицированное чтение без ссылки | Создатель файла или superadmin | Любой member или superadmin |
| Создание файла | Любой аутентифицированный пользователь | `owner`, `editor` или superadmin |
| Изменение существующего файла, lock, кодировка | Создатель файла или superadmin | `owner`, `editor` или superadmin |
| Unlock | Создатель файла, наложивший lock пользователь или superadmin | Создатель файла, наложивший lock пользователь, `owner`, `editor` или superadmin |
| Получение автоматически созданных links / revoke | Создатель файла или superadmin | `owner`, `editor` или superadmin |
| Управление участниками | Не используется | `owner` или superadmin |
| Чтение | Создатель, superadmin или активная ссылка | `owner`, `editor`, `viewer` или superadmin |

В `users` вводится глобальный признак `is_superadmin`. Superadmin управляет пользователями и имеет административный доступ ко всем workspace и файлам; все его действия проходят обычные guards и аудит, кроме проверки membership/авторства. Первоначальный superadmin создаётся только явной идемпотентной bootstrap-командой при развёртывании. Публичная регистрация никогда автоматически не выдаёт эту роль. В приватном workspace сервис обязан запрещать удаление последнего owner.

Список файлов `base` не является публичным каталогом: анонимный запрос списка запрещён, а аутентифицированный пользователь по умолчанию видит только созданные им файлы; superadmin видит все. Список приватного workspace доступен его участникам и superadmin. Для недоступного ресурса по ID рекомендуется возвращать тот же ответ, что и для отсутствующего, чтобы не раскрывать метаданные.

### 2.5. Locks и конкурентность

Hard lock не заменяет optimistic-проверку версии, а update session не заменяет hard lock. Их назначения различны:

- update lock автоматически защищает только update flow;
- hard lock запрещает все операции записи над файлом, но не чтение;
- `409 Conflict` используется для активной update session, stale base, повторного имени и несовместимого перехода статуса;
- `423 Locked` используется только для активного hard lock.

Частичные уникальные индексы по отдельности не предотвращают гонку «создать hard lock одновременно с update session». Обе операции и все write-операции, которые должны уважать hard lock, обязаны сначала блокировать одну и ту же строку `files`.

### 2.6. Links

- Токен — криптографически случайный, непредсказуемый и не содержит ID.
- При создании файла сервис автоматически создаёт одну current-ссылку и version-ссылку для версии 1; при публикации каждой следующей версии в той же транзакции создаётся её version-ссылка.
- Ручного создания ссылок в API/CLI нет. API/CLI только показывают уже созданные ссылки и позволяют их отозвать.
- Current-link разрешает current version в момент каждого запроса.
- Version-link всегда отдаёт зафиксированную версию.
- Ссылка private workspace является только альтернативным идентификатором и требует Bearer token плюс membership.
- Неизвестный и отозванный token должны давать одинаковый публичный ответ, чтобы не раскрывать существование ссылки.
- Токены в URL необходимо редактировать в access logs и telemetry.
- Автоматическое создание и отзыв ссылки являются записью и должны сериализоваться с hard lock через строку файла.
- Revoke необратимо деактивирует конкретный токен и не создаёт ему замену автоматически; запись остаётся для аудита и выполнения инварианта «ссылка создана для каждого файла/версии».

Для MVP допустимо хранение токена как в исходном DDL. Более безопасное хранение только hash токена желательно, но меняет схему; это решение следует принять до первой миграции.

### 2.7. SeaweedFS

- FileStore использует только S3 endpoint и не зависит от внутренних API Master/Volume/Filer.
- Bucket создаётся/проверяется при развёртывании; бизнес-запрос не должен неявно создавать bucket.
- Object key формируется сервером, например по случайному UUID и префиксу назначения; исходное имя не входит в ключ.
- Перезапись существующего ключа приложением запрещена.
- Опубликованные objects не удаляются в MVP.
- Candidate и orphan objects удаляются с повторами; отсутствие объекта при повторном удалении считается успехом.
- Образ SeaweedFS следует закрепить на конкретной проверенной версии, а не использовать `latest`.
- Одноконтейнерный SeaweedFS — сознательная точка отказа MVP. Резервная копия должна включать согласованную пару PostgreSQL + volume SeaweedFS.

### 2.8. Транзакции PostgreSQL

Для MVP достаточно `READ COMMITTED` плюс явные row locks и уникальные ограничения. Обязательные правила:

1. Всегда блокировать сущности в порядке `files` → зависимая сущность (session/lock/link).
2. Повторять проверки прав и состояния после получения блокировки.
3. Не выполнять долгие S3-запросы внутри DB-транзакции.
4. Не считать предварительный `SELECT` защитой от гонки; последняя защита — constraint.
5. Маппить ожидаемые constraint violations в доменные `409`, а не в `500`.
6. После неопределённого результата commit безопасно повторно читать состояние по ID/idempotency key.
7. Member add/remove и защита последнего owner выполняются под блокировкой workspace либо owner rows.

### 2.9. Обнаруженные противоречия

| № | Противоречие | Решение MVP |
|---|---|---|
| C-01 | PNG называет таблицу `file_update_locks`, текст и DDL — `file_locks` | Принято `file_locks`; PNG помечен историческим и заменён актуальной Mermaid ER-схемой в `docs/db_schemas/README.md` |
| C-02 | В PNG `storage_objects.mime_type` обязателен, в DDL nullable | Принято `NOT NULL` с fallback `application/octet-stream` |
| C-03 | Conflict summary относит base version и locks к будущему, основной документ включает их в MVP | Основной документ имеет приоритет |
| C-04 | Conflict summary допускает last-write-wins, основной flow отклоняет stale base и допускает одну active session | Использовать optimistic reject-on-conflict; last-write-wins отсутствует |
| C-05 | «Каждая попытка обновления» хранится в sessions, но сбой до DB insert оставляет только S3 object | Считать попыткой только успешно созданную session; технические сбои фиксировать в логах/метриках |
| C-06 | `base` может использовать `workspace_members`, но права записи описаны через creator/admin | Не использовать membership base в MVP; применять правила из матрицы прав |
| C-07 | Глобальный admin упомянут, но не моделируется | Добавить `users.is_superadmin`, явный bootstrap первоначального superadmin и централизованный административный bypass с аудитом |
| C-08 | CLI содержит list/download/link revoke, но минимальный API не содержит обслуживающих операций | Дополнить API-контракт до реализации CLI |
| C-09 | CLI содержит login, но нет auth contract, credentials или token model | Добавить регистрацию и вход по email/password; API выдаёт opaque Bearer token, а БД хранит password/token hashes |
| C-10 | Cleanup сначала удаляет DB metadata и S3 object без определённого порядка | Применить описанный state-first и retryable cleanup flow |
| C-11 | Активная, но уже истёкшая session продолжает занимать уникальный индекс до cleanup | Проверять время во всех командах и лениво завершать просроченную session |
| C-12 | Документ запрещает rename/delete/change-current под lock, но таких операций в MVP API нет | Не реализовывать их; оставить правило как расширяемую policy |
| C-13 | Workspace создаётся как `AcomOfferDesk`, а CLI далее использует `acom`; slug отсутствует | Внешние операции принимают UUID; `workspace list/create/use` передают полученный UUID, slug отсутствует |
| C-14 | `UNIQUE(workspace_id, name)` чувствителен к регистру, а CLI работает на разных ОС | Рекомендуется регистронезависимая уникальность имени файла внутри workspace |
| C-15 | Запись `UNIQUE(lower(email))` внутри определения таблицы не является допустимым expression constraint PostgreSQL | Принято `email citext UNIQUE`; workspace/file names используют отдельные expression indexes |
| C-16 | Status допускает несовместимые nullable-поля session и lock | Добавить status-dependent CHECK constraints и единый state transition service |
| C-17 | Документ допускает `INSERT users`, но не описывает регистрацию/первичную identity | Пользователь создаётся регистрацией и затем входит; первоначальный superadmin создаётся отдельной идемпотентной bootstrap-командой и также использует обычный login |
| C-18 | История индексируется по `created_at`, хотя нормативный порядок задаёт `version_number` | Сортировать историю по `version_number DESC`; timestamp оставить отображаемым атрибутом |

## 3. Найденные неоднозначности

| ID | Не задано | Почему важно | Простое решение для MVP |
|---|---|---|---|
| A-01 | Язык и библиотеки | Определяет структуру, миграции и тесты | Go 1.25+, `net/http`, standard `flag`, `pgx/v5`; AWS SDK for Go v2 для S3-compatible adapter |
| A-02 | Способ аутентификации и выдачи token | Без этого private workspace небезопасен, а `login` не реализуем | Регистрация и вход по email/password; пароль хранится как стойкий password hash, login выдаёт opaque Bearer token, в БД хранится только hash token |
| A-03 | Создание первого user и superadmin | Невозможно безопасно начать администрирование | Идемпотентная deployment-команда `filestore-api bootstrap-superadmin`; публичная регистрация не назначает роль автоматически, созданный superadmin входит обычной командой login |
| A-04 | Полный API-контракт upload/download/list/revoke | CLI не может работать | Зафиксировать недостающие операции в OpenAPI до handler-ов |
| A-05 | Формат upload | Влияет на streaming и лимиты | Один multipart request через API; presigned/multipart-to-S3 исключён |
| A-06 | Максимальный размер файла | Защита памяти, диска и времени запроса | Потоковый hard limit, default 100 MiB; `FILESTORE_MAX_FILE_SIZE` |
| A-07 | TTL update session | Определяет UX и cleanup | Default 24 часа; `FILESTORE_UPDATE_SESSION_TTL`, server clock authoritative |
| A-08 | Поддерживаемый diff | Иначе endpoint непроверяем | Unicode line diff: до 1 MiB decoded bytes и 20 000 строк на сторону, output до 1 MiB; иначе полный metadata-only result с причиной |
| A-09 | Значение «риск отката» | В основном документе термин не определён | Совпадение hash кандидата с более старой версией |
| A-10 | Поведение при одинаковом с current содержимом | Нет статуса `noop` | Создать обычную active session с пустым diff; пользователь resolve/reject |
| A-11 | Idempotency create/upload | Повторы сети могут создавать объекты/сессии | Поддержать idempotency key минимум для первой загрузки, create session и resolve |
| A-12 | Кто может unlock | Иначе пользователь может снять чужой lock | Создатель файла, locker или editor; owner включает права editor, superadmin имеет административный override |
| A-13 | Кто создаёт/отзывает links | Не определено явно | Links создаёт сервис автоматически; base creator/private owner/editor/superadmin просматривают/отзывают; 256-bit token хранится plaintext для повторного просмотра и строго редактируется в логах |
| A-14 | Поведение revoked/expired/not found | Влияет на security и CLI | Единый problem format; публичные tokens маскировать как not found, expired session возвращать доменную ошибку |
| A-15 | Version selector | CLI использует номер, БД — UUID | Внешний контракт принимает version number в контексте file; API сам находит version ID |
| A-16 | Имя логического файла | Local path, original_name и files.name различаются | `files.name` задаётся явно или basename первой загрузки; `original_name` сохраняет basename каждого upload |
| A-17 | Кодировка текста | Нельзя безопасно строить line diff, а ошибочное определение ломает чтение | Allowlist `utf-8`, `utf-16le`, `utf-16be`, `windows-1251`; `utf-8` обязателен/default; смена metadata не создаёт версию и не меняет bytes |
| A-18 | Согласованность backup | PostgreSQL и SeaweedFS меняются независимо | На MVP — maintenance window и остановка write-трафика на время согласованной копии |
| A-19 | Политика orphan retention | Немедленное удаление опасно при гонках | Удалять только повторно проверенные несвязанные объекты старше default grace period 24 часа |
| A-20 | Наблюдаемость cleanup | Потерянные objects иначе незаметны | Метрики количества active/expired/orphan и ошибок удаления; структурные логи с IDs без token |
| A-21 | Кто завершает чужую update session | Editor может принять/отклонить кандидата другого пользователя | Session creator или workspace owner; в base — creator файла; superadmin имеет административный override |

## 4. Предлагаемые уточнения перед разработкой

Ниже — решения, которые рекомендуется утвердить как часть MVP baseline:

1. Основной режим конфликтов — `reject on stale base`, не last-write-wins.
2. Технологический стек — Go; API и CLI в одном модуле, два бинарника.
3. Auth — регистрация и вход по email/password. API хранит стойкий password hash, выдаёт opaque Bearer token и хранит только его hash. Первоначальный superadmin создаётся отдельной идемпотентной bootstrap-командой и входит тем же способом, что обычный пользователь.
4. Base write policy — любой authenticated user может создать новый файл; далее изменять его может creator. Superadmin имеет глобальный административный доступ с обязательным аудитом.
5. Private policy — owner/editor пишут, viewer читает, только owner управляет membership; последний owner не удаляется.
6. File names и workspace names уникальны регистронезависимо; директорий и path hierarchy нет.
7. Diff — Unicode line diff при лимитах 1 MiB/20 000 строк на сторону и 1 MiB output после декодирования согласно allowlist; иначе metadata-only с причиной. Смена кодировки не меняет bytes и не создаёт версию. Merge отсутствует.
8. Update TTL по умолчанию 24 часа, upload limit 100 MiB, orphan grace period 24 часа; значения задаются конфигурацией API. Серверное время является единственным источником истины.
9. Resolve идемпотентен. Для create-file и create-session CLI отправляет idempotency key.
10. API расширяется только операциями, необходимыми уже заявленным CLI: register/login/logout, поиск/выбор workspace, list files, authenticated download, получение/отзыв автоматически созданных links, смена кодировки и проверка текущей identity.
11. Cleanup встроен в API-процесс для single-instance MVP. При масштабировании применяется DB coordination, а затем при необходимости выделяется worker.
12. SeaweedFS image pin фиксируется в deployment-документации; bucket и credentials создаются вне бизнес-запросов.
13. Current-link создаётся автоматически для каждого файла, version-link — для каждой опубликованной версии; пользовательского create-link endpoint нет.
14. Unlock разрешён создателю файла, пользователю, наложившему lock, и editor (owner наследует его права); superadmin имеет административный override.
15. Link token — 256-bit base64url capability, хранится plaintext ради авторизованного повторного просмотра; БД/backup считаются secret-bearing, token редактируется во всех логах.
16. Standalone user lifecycle/admin API и HTTP Range явно отложены за пределы MVP; bootstrap и superadmin workspace/file override покрывают заявленный MVP.

Все решения A-01–A-21 утверждены. Точные operational defaults и capability-token policy закреплены ADR 0004 и отражены в OpenAPI/config; изменения требуют нового ADR.

## 5. Dependency Map

```text
Configuration ───────────────┐
Clock / UUID / Logging ──────┼──────────────┐
                             v              v
Domain rules <── Repository interfaces   Storage interface
     ^                    ^                   ^
     |                    |                   |
     |          PostgreSQL repositories   SeaweedFS S3 adapter
     |                    ^                   ^
     └──────── Services / Authorization / Transactions
                              ^              ^
                              |              |
                    HTTP API handlers     Cleanup scheduler
                              ^
                              |
                          OpenAPI contract
                              ^
                              |
                    API client used by CLI
                              ^
                              |
                  CLI commands + local config
```

| Подсистема | Зависит от | Не должна зависеть от |
|---|---|---|
| Configuration | env/files/flags | Бизнес-сервисов, DB records |
| Domain | Только стандартных типов | HTTP, PostgreSQL, S3, CLI |
| Repository interfaces | Domain | Конкретного PostgreSQL driver в сигнатурах бизнес-слоя |
| PostgreSQL repositories | Config, DB driver, domain/repository contracts | HTTP и CLI |
| Storage interface | Domain value types | AWS-specific types в сервисах |
| SeaweedFS adapter | Config, S3 SDK, storage contract | Workspace/access rules |
| AuthN middleware | Token repository, clock | Workspace policy |
| Authorization | Identity + domain/repositories | HTTP transport |
| File/version service | Repositories, storage, authz, clock/UUID/hash | CLI |
| Update service | File/version repositories, session repository, storage, diff, authz | HTTP details |
| Lock/link/workspace services | Соответствующие repositories, authz | S3, кроме link download service |
| Cleanup | Session/storage repositories, storage adapter, clock | HTTP handlers |
| REST API | Services, auth middleware, OpenAPI types | SQL/S3 SDK напрямую |
| API client | OpenAPI contract, HTTP client | DB/S3 |
| CLI | API client, local config, terminal rendering | DB/S3/domain repositories |
| Composition root | Все реализации для wiring | Бизнес-логики |

Ключевое правило связанности: handler не открывает транзакцию и не вызывает S3 напрямую; repository не принимает решение о правах; CLI не дублирует серверные правила.

## 6. Архитектура проекта

```text
cmd/
    filestore-api/
    filestore/

internal/
    app/
    api/
        http/
        middleware/
        problem/
    auth/
    authorization/
    cleanup/
    cli/
    client/
    config/
    database/
    diff/
    encoding/
    domain/
    repository/
        postgres/
    service/
    storage/
        seaweedfs/
    observability/

migrations/
openapi/
tests/
    integration/
    e2e/
    fixtures/
docs/
    stages/
```

### 6.1. Назначение директорий

| Директория | Назначение |
|---|---|
| `cmd/filestore-api` | Точка запуска API, wiring, lifecycle фонового cleanup, graceful shutdown |
| `cmd/filestore` | Точка запуска CLI |
| `internal/app` | Composition root; сборка зависимостей без бизнес-логики |
| `internal/api/http` | Реализация OpenAPI handlers, streaming response/request |
| `internal/api/middleware` | Authentication, request ID, logging, recovery, limits |
| `internal/api/problem` | Единая модель ошибок и HTTP mapping |
| `internal/auth` | Регистрация, password hashing, login/logout, проверка Bearer token и получение identity |
| `internal/authorization` | Централизованная workspace/file permission policy |
| `internal/cleanup` | Expiry sessions, candidate/orphan deletion, periodic reconciliation |
| `internal/cli` | Разбор команд, вывод diff/ошибок, local workspace selection |
| `internal/client` | Типизированный HTTP-клиент по OpenAPI |
| `internal/config` | API, DB, S3, limits, TTL и CLI local/global configuration |
| `internal/database` | Pool, transaction runner, migrations/startup checks |
| `internal/diff` | Ограниченный text diff и metadata comparison |
| `internal/encoding` | Нормализация поддерживаемых кодировок и безопасное декодирование в Unicode |
| `internal/domain` | Сущности, статусы, value objects, доменные ошибки и инварианты |
| `internal/repository` | Узкие интерфейсы persistence по use case |
| `internal/repository/postgres` | PostgreSQL-реализации и блокирующие запросы |
| `internal/service` | Use cases и порядок DB/S3 действий/компенсаций |
| `internal/storage` | Абстракция immutable object store |
| `internal/storage/seaweedfs` | S3-compatible adapter SeaweedFS |
| `internal/observability` | Структурные логи и минимальные метрики |
| `migrations` | Последовательные неизменяемые миграции schema/seed base |
| `openapi` | Единственный контракт API и схем ошибок |
| `tests/integration` | Реальные PostgreSQL и SeaweedFS |
| `tests/e2e` | API + CLI пользовательские сценарии |
| `tests/fixtures` | Малые text/binary fixtures без production-данных |
| `docs/stages` | По одному Markdown-файлу на этап с инструкцией для ручного запуска, проверки результата, негативных сценариев и очистки тестовых данных |
| `docs` | Архитектура, ADR, runbooks, актуальная схема данных |

`pkg/` не нужен: MVP не предоставляет публичную Go-библиотеку. Универсальный generic repository тоже не нужен; интерфейсы формируются вокруг транзакционных use cases.

### 6.2. API/CLI gap analysis

| CLI-функция | Контракт из документа | Состояние |
|---|---|---|
| config, workspace use | Локальный | API не нужен |
| register/login/logout | Не описан | Нужны регистрация и вход по email/password, выдача/отзыв opaque token и проверка identity |
| workspace show-base/create | Есть | Достаточно после уточнения response schemas |
| member add/remove | Есть | Add по email должен однозначно создавать/находить user |
| upload/info/history | Есть | Нужно определить multipart и response schemas |
| list | Нет | Нужна операция списка файлов workspace с простой пагинацией |
| authenticated download current/version | Нет | Нужна content-операция по file и optional version number |
| encoding get/set | Нет | Нужна операция чтения/смены `text_encoding` без изменения bytes и создания версии |
| update/diff/resolve/reject | Есть | Нужны state/error/idempotency contracts |
| lock/unlock/status | Есть | Достаточно после уточнения прав |
| links list | Частично описан create | Ручной create исключается; API возвращает автоматически созданные current/version links |
| link revoke | Нет | Нужна операция отзыва по link ID |
| workspace use по имени | Нет lookup | Нужен list/lookup workspace либо CLI должен хранить UUID из create |

До реализации handler-ов эти пробелы должны быть устранены в `openapi/openapi.yaml`. Это не расширение продукта: операции уже требуются заявленным CLI.

## 7. План реализации по этапам

Каждый этап заканчивается компилируемым репозиторием, зелёными тестами и рабочим предыдущим функционалом. Миграции после попадания в общую ветку не переписываются.

Для каждого этапа создаётся отдельный файл-описание `docs/stages/NN-<name>.md`. Это не исполняемый скрипт, а воспроизводимая инструкция ручной проверки. В каждом файле обязательны: цель и границы этапа, предусловия и зависимости, команды запуска приложения и автоматических тестов, подготовка тестовых данных, пошаговый позитивный сценарий, негативные проверки, ожидаемые результаты, очистка данных и итоговый checklist. Ручная проверка является накопительной: кроме новой функции подтверждается работоспособность ключевого сценария предыдущих этапов. Пароли и tokens в примеры не встраиваются — используются placeholders и ссылки на test profile.

### Этап 0. Контракт и компилируемый каркас

**Цель:** зафиксировать принятые решения и получить запускаемые API/CLI без бизнес-функций.

**Компоненты:** модуль Go, composition roots, config skeleton, единый error format, OpenAPI baseline, CI/lint/test commands.

**Будущие файлы:** `go.mod`, `cmd/filestore-api/main.go`, `cmd/filestore/main.go`, `internal/app/*`, `internal/config/*`, `internal/api/problem/*`, `internal/cli/root.go`, `openapi/openapi.yaml`, `docs/stages/00-contract.md`, ADR по auth/permissions/transactions.

**Миграции:** нет.  
**API:** продуктовые операции только задокументированы и ещё не активны; процесс проверяет конфигурацию и корректно запускается/останавливается без добавления бизнес-endpoint-ов.  
**CLI:** help, version, config get/set.  
**Тесты:** config precedence, CLI parsing, problem serialization, OpenAPI validation.

**Definition of Done:** оба бинарника собираются и корректно завершаются; спецификация OpenAPI проходит validation; решения A-01–A-21 приняты либо явно отложены как не влияющие на текущий этап; ручная проверка выполнена по `docs/stages/00-contract.md`; CI выполняет lint/unit/build.

### Этап 1. PostgreSQL, identity и workspace

**Цель:** получить аутентифицированную работу с base/private workspaces.

**Компоненты:** DB pool/transaction runner, migration runner, регистрация и login/logout, password/token hashing, bootstrap superadmin, authorization base, workspace service/repository.

**Будущие файлы:** `internal/database/*`, `internal/auth/*`, `internal/authorization/*`, `internal/domain/user.go`, `internal/domain/workspace.go`, `internal/repository/postgres/user*.go`, `workspace*.go`, workspace/auth handlers и CLI-команды, `docs/stages/01-identity-workspace.md`.

**Миграции:** `000001_users_workspaces` с password hash и `is_superadmin`; `000002_user_tokens` с hash opaque token. Первая миграция также создаёт зарезервированный base идемпотентно, но не содержит пароль или готового пользователя.

**API:** register, login, logout/revoke token, проверка текущей identity; create/list/get-default workspace; add/remove member; superadmin user administration.  
**CLI/operations:** пользовательские команды register, login, logout, workspace show-base/create/member add/member remove/use; локальная deployment-команда `filestore-api bootstrap-superadmin`, недоступная как публичный API endpoint.  
**Тесты:** register/login/logout, password/token не попадают в логи, case-insensitive names/emails, конкурентная повторная регистрация, идемпотентный bootstrap superadmin, отсутствие автоматического повышения первого registrant, base singleton, owner creation в одной транзакции, роли и superadmin override, last-owner protection, invalid/revoked token, anonymous denial.

**Definition of Done:** после чистой миграции существует один base; пользователь может зарегистрироваться и войти; bootstrap создаёт ровно одного заданного superadmin и безопасно повторяется; private workspace создаётся вместе с owner; role matrix выполняется; секреты CLI не выводятся в логи; интеграционные тесты и ручная проверка по `docs/stages/01-identity-workspace.md` проходят на реальном PostgreSQL.

### Этап 2. SeaweedFS adapter и первая версия файла

**Цель:** полный вертикальный сценарий upload → metadata/history → authenticated download.

**Компоненты:** immutable storage interface, S3 adapter, hashing/stream limits, file/version service, изменяемая метаинформация кодировки, list/info/history/download, базовый orphan reconciliation.

**Будущие файлы:** `internal/storage/storage.go`, `internal/storage/seaweedfs/*`, `internal/domain/file.go`, `version.go`, `storage_object.go`, `internal/encoding/*`, `internal/repository/postgres/file*.go`, `version*.go`, `storage_object*.go`, `internal/service/file*.go`, `internal/cleanup/orphans.go`, handlers/client/CLI для файлов, `docs/stages/02-files-versions.md`.

**Миграции:** `000003_storage_files_versions`, включая `files.text_encoding`, индексы, регистронезависимую уникальность имени и отложенное добавление current-version integrity constraint.

**API:** create file/first version, list files, file info/current, version history, authenticated content download, get/update file encoding.  
**CLI:** upload, list, info, history, download current/`--version`, encoding get/set.  
**Тесты:** streaming hash/size, MIME fallback, zero-byte file, size limit, duplicate name race, default/явная/неподдерживаемая кодировка, смена кодировки без изменения bytes/version number и проверка прав, transaction rollback + S3 compensation, DB-only и S3-only orphan detection, missing Seaweed object, version ownership, range support только если оно включено в контракт.

**Definition of Done:** загруженный файл скачивается byte-for-byte; первая версия имеет номер 1 и является current; кодировку можно исправить без изменения bytes и создания версии; viewer не загружает; сбой DB после S3 upload не оставляет постоянную невидимую утечку; API/CLI e2e и ручная проверка по `docs/stages/02-files-versions.md` проходят для base creator и private member.

### Этап 3. Создание update session и diff

**Цель:** безопасно принять ровно одного кандидата и показать сравнение без изменения current.

**Компоненты:** update session domain/repository/service, candidate upload, limited diff, rollback warning, idempotency для create-session.

**Будущие файлы:** `internal/domain/update_session.go`, `internal/repository/postgres/update_session*.go`, `internal/service/update_create.go`, `internal/diff/*`, update handlers/client/CLI, `docs/stages/03-update-diff.md`.

**Миграции:** `000004_file_update_sessions` с status checks, partial unique index, expiry index и принадлежностью base/resolved versions файлу; idempotency field/table согласно принятому ADR.

**API:** create update session, get diff.  
**CLI:** update, update diff.  
**Тесты:** concurrent create (ровно один успех), hard size limit, base=current фиксация под lock, duplicate retry, UTF-8 и альтернативная поддерживаемая кодировка, повтор diff после исправления кодировки, invalid-sequence/binary/large fallback, hash совпадает с current/старой версией, S3/DB failure compensation, access denial.

**Definition of Done:** current version не меняется; на файл не более одной active session; проигравшие гонку objects удаляются или обнаруживаются как orphan; diff имеет ограниченный по размеру стабильный ответ; ручная проверка выполнена по `docs/stages/03-update-diff.md`.

### Этап 4. Resolve, reject и session cleanup

**Цель:** завершить lifecycle update session и обеспечить eventual cleanup кандидатов.

**Компоненты:** state transition service, resolve/reject, expiry scheduler, orphan reconciliation, retry policy.

**Будущие файлы:** `internal/service/update_resolve.go`, `update_reject.go`, `internal/cleanup/sessions.go`, расширение orphan rules для session candidates, lifecycle wiring в `internal/app`, `docs/stages/04-update-lifecycle.md`.

**Миграции:** обычно нет; корректирующая миграция допустима только для выявленного до merge constraint/index.  
**API:** resolve update, reject/delete update.  
**CLI:** update resolve, update reject.  
**Тесты:** resolve success, repeat resolve idempotency, reject repeat, resolve-vs-reject race, expired resolve, stale base, monotonic version numbers, candidate preserved after resolve, cleanup deletion failure/retry, cleanup-vs-resolve race, restart scheduler.

**Definition of Done:** каждая session приходит ровно в один terminal status; resolve атомарно создаёт одну published version; reject/expire никогда не меняет file; ошибки SeaweedFS не возвращают session в active и остаются повторяемыми cleanup-ом; ручная проверка выполнена по `docs/stages/04-update-lifecycle.md`.

### Этап 5. Полные блокировки

**Цель:** реализовать file-level запрет записи, совместимый с update sessions.

**Компоненты:** lock domain/repository/service, единая write guard policy для всех уже реализованных операций.

**Будущие файлы:** `internal/domain/file_lock.go`, `internal/repository/postgres/file_lock*.go`, `internal/service/lock*.go`, shared write guard, lock handlers/client/CLI, `docs/stages/05-hard-locks.md`.

**Миграции:** `000005_file_locks` с partial unique active-lock index.  
**API:** lock, unlock, lock status/history в объёме контракта.  
**CLI:** lock, unlock, lock-status.  
**Тесты:** concurrent lock, lock-vs-create-session race, lock-vs-resolve race, все существующие writes, включая смену кодировки, возвращают 423; reads/download/history remain available; unlock разрешён создателю файла, locker, editor/owner и superadmin и запрещён остальным; preserved history.

**Definition of Done:** hard lock и active session не сосуществуют; ни одна write-operation не обходит guard; права unlock соответствуют утверждённому правилу; конкурентные тесты подтверждают сериализацию по files row; история release сохранена; ручная проверка выполнена по `docs/stages/05-hard-locks.md`.

### Этап 6. Ссылки

**Цель:** автоматически обеспечить current-link для каждого файла и version-link для каждой опубликованной версии с разным поведением base и private workspaces.

**Компоненты:** token generation/storage, транзакционное автоматическое создание/backfill links, link service/repository, anonymous/authenticated content handler, listing и revocation.

**Будущие файлы:** `internal/domain/file_link.go`, `internal/repository/postgres/file_link*.go`, `internal/service/link*.go`, public-link handler, client/CLI link commands, token redaction middleware, `docs/stages/06-links.md`.

**Миграции:** `000006_file_links` с token uniqueness, уникальностью одной current-ссылки на файл и одной version-ссылки на версию, проверяемой принадлежностью version файлу и идемпотентным backfill для всех существующих файлов/версий.  
**API:** list/get автоматически созданных links, revoke link, download by token; create-link endpoint отсутствует.  
**CLI:** link list/show, link revoke; команда `link create` отсутствует.  
**Тесты:** автоматические current+v1 при создании файла, автоматическая version-link при resolve, backfill существующих записей, retry без дубликатов, anonymous base current, immutable version link, current changes after resolve, private anonymous denial, private member success, revoked/unknown indistinguishable, wrong-file version rejection, token collision retry, hard lock blocks revoke, token absent in logs.

**Definition of Done:** у каждого файла есть ровно одна созданная current-ссылка, у каждой версии — ровно одна созданная version-ссылка; создание файла и resolve не фиксируются без требуемых ссылок; base link скачивает правильные bytes; private link не ослабляет membership; revoke немедленно прекращает выдачу и не создаёт замену; version link остаётся неизменным после новых версий; ручная проверка выполнена по `docs/stages/06-links.md`.

### Этап 7. Release hardening

**Цель:** доказать целостность MVP и подготовить эксплуатацию.

**Компоненты:** полные e2e, metrics/logging, graceful shutdown, migration compatibility, backup/restore and orphan cleanup runbooks, API/CLI documentation.

**Будущие файлы:** `tests/e2e/*`, `tests/integration/concurrency_*`, `internal/observability/*`, `docs/runbooks/backup-restore.md`, `cleanup.md`, `docs/permissions.md`, обновлённая DB diagram, `docs/stages/07-release.md`.

**Миграции:** только additive fixes; schema snapshot документируется, но не становится альтернативным источником истины.  
**API/CLI:** весь заявленный MVP, без новых продуктовых функций.  
**Тесты:** чистая установка, upgrade с каждой выпущенной миграции, полный CLI journey, отказ PostgreSQL/SeaweedFS, restart между S3 и DB шагами, backup/restore drill, race suite.

**Definition of Done:** все уровни тестов зелёные; OpenAPI и CLI help синхронны; нет известных permanent orphan leaks; восстановление согласованной пары проверено; образ SeaweedFS и зависимости закреплены; ручная проверка release candidate выполнена по `docs/stages/07-release.md`; release checklist пройден.

### 7.1. Почему порядок именно такой

Workspace/auth предшествуют файлам, потому что авторство и права входят в каждую последующую запись. Storage и базовая версия предшествуют updates, потому что session всегда ссылается на существующую current version и candidate object. Update flow разделён на создание/diff и terminal transitions, чтобы отдельно проверить наиболее рискованную границу DB↔S3. Hard lock добавляется после рабочего update flow, затем единый write guard проверяется на всех операциях. Links идут последними среди бизнес-функций, потому что зависят от downloads, versions, permissions и hard lock; их миграция делает backfill, а затем file-create/resolve атомарно создают links для всех новых записей. CLI развивается вертикально вместе с API, а не откладывается целиком на конец, поэтому расхождения контракта обнаруживаются рано.

## 8. План тестирования

### 8.1. Unit tests

Без PostgreSQL и SeaweedFS проверяют:

- state machine update session;
- permission matrix base/private/superadmin и unlock policy;
- автоматическое создание и version/current selection links;
- нормализация кодировок, text-vs-binary diff, limits и rollback warning;
- token parsing/redaction;
- error mapping;
- CLI argument validation и rendering;
- compensation decisions при моделируемых ошибках.

Критические ошибки: неверный переход terminal status, обход viewer restriction, версия другого файла, утечка token в лог, неограниченный diff, повтор resolve создаёт вторую версию.

### 8.2. Integration tests

С реальными PostgreSQL и SeaweedFS проверяют:

- миграции с нуля и ограничения schema;
- register/login/logout, идемпотентный bootstrap superadmin и отсутствие неявного повышения роли;
- транзакции, row locks, unique conflicts и rollback;
- byte-for-byte upload/download;
- поведение при недоступном S3/DB;
- cleanup и orphan reconciliation;
- параллельные запросы на session/resolve/lock/member removal;
- транзакционное создание/backfill links без пропусков и дубликатов;
- S3 compensation после DB failure.

Критические ошибки: две active sessions, hard lock вместе с session, два одинаковых version numbers, current указывает на чужую version, DB metadata без объекта после успешного ответа, удаление published object.

### 8.3. End-to-end tests

Через собранный CLI и реальный API проверяют:

1. Настройка CLI, регистрация/login, bootstrap и login superadmin, получение base.
2. Создание private workspace, добавление editor/viewer.
3. Upload, list, info, history, download и сравнение bytes; исправление кодировки не меняет bytes/version.
4. Update → diff → исправление кодировки → повтор diff → resolve → новая current/history.
5. Update → reject и update → expire.
6. Hard lock блокирует запись, но не download; creator/locker/editor может выполнить unlock, посторонний пользователь — нет.
7. Автоматические base current/version links доступны анонимно; private link — только member; revoke.
8. Повтор команды после симулированного потерянного HTTP-ответа.

Критические HTTP-классы: 400 invalid input, 401 no/invalid token, 403 insufficient role, 404 hidden/not found, 409 concurrent/stale/state conflict, 413 too large, 423 hard lock, 5xx storage/database unavailable.

### 8.4. Тесты по этапам

| Этап | Unit | Integration | E2E | Инструкция ручной проверки |
|---|---|---|---|---|
| 0 | Config, errors, CLI parser | Не обязательны | API/CLI start/stop | `docs/stages/00-contract.md` |
| 1 | Auth/permission/superadmin rules | Migrations, registration/login, bootstrap, roles, last owner | Register/login + workspace journey | `docs/stages/01-identity-workspace.md` |
| 2 | File/version/encoding validation | S3 streaming, atomic first version, encoding update | Upload/download/history/encoding | `docs/stages/02-files-versions.md` |
| 3 | Encoding-aware diff, update validation | Concurrent session creation | Update + encoding fix + diff | `docs/stages/03-update-diff.md` |
| 4 | State transitions | Resolve/reject/cleanup races | Resolve/reject/expire | `docs/stages/04-update-lifecycle.md` |
| 5 | Write guard и unlock policy | Lock concurrency | Lock/unlock with reads/writes | `docs/stages/05-hard-locks.md` |
| 6 | Automatic link resolution | Backfill/token/version constraints, no duplicates | Automatic anonymous/private/revoke | `docs/stages/06-links.md` |
| 7 | Regression | Failure/recovery/race suite | Full MVP and restore drill | `docs/stages/07-release.md` |

## 9. Основные риски

| Риск | Проявление | Вероятность/влияние | Минимизация |
|---|---|---|---|
| Нет общей транзакции DB+S3 | Orphan object или metadata без bytes | Высокая/высокое | S3-first для upload, DB transaction, compensation, orphan reconciliation, fault-injection tests |
| S3-only orphan до DB commit | Object существует, но `storage_objects` о нём не знает | Средняя/среднее | Уникальные датированные prefixes, bucket inventory с grace period, immediate compensation и метрика расхождений |
| Check-then-act между двумя lock tables | Одновременно hard lock и active session | Средняя/высокое | Общая row lock на `files`, повторные проверки, unique indexes |
| Resolve/reject/cleanup race | Удаление публикуемого кандидата или двойной terminal status | Средняя/высокое | Единый порядок locks, условные transitions только из active, expiry check под lock |
| Duplicate HTTP retry | Две версии/objects после timeout | Высокая/среднее | Idempotency key, идемпотентный resolve, stable response lookup |
| Stale base | Потеря изменений | Средняя/высокое | `current == base` под files row lock, 409, candidate retained/rejected per explicit flow |
| Cross-file FK | Version/link/session другого файла | Низкая/критическое | Composite integrity constraints и negative tests |
| Cleanup удаляет нужный object | Потеря опубликованной версии | Низкая/критическое | Grace period, recheck references under transaction, never delete referenced/published, dry-run metrics |
| Active expired session блокирует файл | Пользователь не может обновить до scheduler tick | Высокая/среднее | Lazy expiry на write endpoints плюс periodic cleanup |
| Seaweed single container | Полная недоступность/потеря volume | Средняя/высокое | Pin version, persistent volume, health/readiness, backup/restore drill; HA вне MVP |
| Seaweed/S3 semantics mismatch | SDK операция работает иначе, чем AWS S3 | Средняя/среднее | Integration tests именно с выбранной SeaweedFS version; использовать минимальный набор Put/Get/Delete/Head |
| Большие файлы/diff | OOM, timeout, занятые connections | Высокая/высокое | Streaming, hard limits, bounded diff, timeouts, не держать DB tx во время transfer |
| Token в URL/logs | Несанкционированный public access | Средняя/высокое | High entropy, TLS, log redaction, no query propagation, revoke, по возможности hash at rest |
| Неверная auth-модель | Имя/email подменяют identity либо первый registrant получает лишние права | Высокая/критическое | Password/token hashing, Bearer identity как единственный источник user ID, отдельный идемпотентный bootstrap superadmin без auto-promotion |
| Ошибка кодировки | Текст читается неверно или diff вводит в заблуждение | Средняя/среднее | Изменяемая метаинформация кодировки, allowlist декодеров, metadata-only при ошибке, тест неизменности bytes/version |
| Пропущенная автоматическая ссылка | Файл/версия существуют без обещанного способа совместного доступа | Средняя/высокое | Создание link в транзакции публикации, unique constraints, идемпотентный backfill и invariant tests |
| Owner removal race | Workspace остаётся без owner | Низкая/высокое | Workspace/owner row lock и constraint-level/integration checks |
| Case-sensitive collisions | Разное поведение CLI на ОС | Средняя/среднее | Регистронезависимые unique rules и явное отображаемое имя |
| Backup несогласован | DB ссылается на отсутствующие objects после restore | Средняя/критическое | Остановить writes/согласованно заморозить, маркировать пару backup одним ID, restore validation |
| Рост истории | Медленные history/orphan scans, объём storage | Низкая для MVP/среднее | Индексы, пагинация, batch cleanup, retention не вводить без продуктового решения |
| Deadlocks PostgreSQL | Транзакции взаимно ждут files/session/lock | Средняя/среднее | Один порядок блокировки, короткие tx, retry только для известных transient DB errors, concurrency tests |

### 9.1. Масштабирование

MVP не должен заранее строить распределённый scheduler или HA Seaweed cluster. Без изменения доменной модели масштабирование возможно так:

- API replicas разделяют PostgreSQL и SeaweedFS;
- row locks и constraints остаются механизмом координации;
- cleanup использует batch claim/skip-locked либо advisory coordination;
- downloads при росте нагрузки позже могут перейти на короткоживущие presigned URLs, но сейчас проходят через API из-за access rules;
- history/list получают cursor pagination до появления больших наборов;
- object storage и DB масштабируются независимо, но backup остаётся согласованным процессом.

## 10. Обязательные ограничения реализации

1. ADR 0001–0004, OpenAPI и `docs/db_schemas/README.md` являются утверждённым baseline языка/стека, auth, permissions, link token, encoding, diff, limits и TTL.
2. Исторические DOCX/PNG не изменяются как бинарные первоисточники; все C-01–C-18 разрешены этим планом и актуальной version-controlled ER-схемой.
3. OpenAPI обновляется раньше handlers и остаётся контрактом request/response, implementation status и problem format.
4. Создать миграции только после фиксации auth и composite integrity constraints: ранняя ошибка schema дороже сервисной правки.
5. Не вводить dedup, merge, directories, rollback, soft locks, presigned upload, полноценную audit-подсистему или отдельный worker — они не нужны MVP; для superadmin достаточно обязательных структурированных security events.
6. Централизовать permission policy, write guard и session state transitions; не размазывать их по handlers.
7. Не создавать generic storage/repository abstractions «на будущее»: нужны только узкие операции текущих use cases.
8. Сделать object keys случайными и immutable; исходное имя хранить только в PostgreSQL/response headers.
9. Пиновать SeaweedFS и проверять S3 adapter интеграционными тестами на этой же версии.
10. Встроить fault injection вокруг границы S3/DB с этапа первой загрузки, а не после завершения функций.
11. Добавить минимальные метрики: active/expired sessions, hard locks, orphan count, cleanup failures, S3 latency/errors, DB tx conflicts.
12. Обновлять этот план и checklist после каждого принятого уточнения; OpenAPI и migrations остаются техническими источниками истины реализации.
13. Поддерживать `docs/stages/00-*.md`–`07-*.md` вместе с кодом: команды и ожидаемые результаты ручной проверки должны соответствовать актуальным API/CLI, миграциям и автоматическим тестам.

## 11. Рабочий чек-лист разработки

Статус реализации на 16.07.2026: **D-00–D-08 доведены до минимально рабочего тестируемого MVP**. Реализована вертикаль identity/workspace → PostgreSQL/SeaweedFS files → update/diff/resolve/reject/expire → hard locks → automatic links через HTTP API, Go client и CLI. Автоматическая приёмка использует реальные PostgreSQL и SeaweedFS; инструкции находятся в `docs/stages/00-*.md`–`07-release.md`. Production-наблюдаемость, нагрузочный профиль и restore drill на целевой инфраструктуре остаются отдельной эксплуатационной работой и не входят в утверждение о локально тестируемом MVP.

| ID | Этап | Статус | Зависимости | Критерии готовности |
|---|---|---|---|---|
| D-00 | Утвердить ADR и устранить неоднозначности | Завершён | Нет | Решены A-01–A-21, C-01–C-18 отражены в спецификации |
| D-01 | OpenAPI и компилируемый каркас | Завершён | D-00 | API/CLI собираются; contract validation/lint/unit зелёные; ручная проверка пройдена по `docs/stages/00-contract.md` |
| D-02 | PostgreSQL/миграции/identity/workspace | Завершён | D-01 | Register/login/bootstrap superadmin, roles и workspace API/CLI проверены |
| D-03 | SeaweedFS + files + first versions | Завершён | D-02 | Atomic upload, current v1, encoding, history/download и compensation проверены |
| D-04 | Update session creation + diff | Завершён | D-03 | Один active candidate, current неизменен, bounded encoding-aware diff проверен |
| D-05 | Resolve/reject/expire/cleanup | Завершён | D-04 | Terminal transitions, idempotent resolve, scheduler и orphan cleanup реализованы |
| D-06 | Hard locks | Завершён | D-05 | Write guard, unlock policy и lock/session race проверены |
| D-07 | Links | Завершён | D-03, D-06 | Automatic current/version links, base/private access и revoke реализованы |
| D-08 | Минимальная release-проверка | Завершён | D-02–D-07 | Clean verification, real-infra journey, pinned dependencies и runbook готовы |

### Детальный контроль выполнения

- [x] **D-00 — завершён:** ADR 0001–0004 фиксируют Go stack, auth/bootstrap, permissions, транзакции, upload 100 MiB, update TTL 24h, diff 1 MiB/20 000 строк/1 MiB output, encoding allowlist, orphan grace 24h и plaintext 256-bit link capability; C-01–C-18 разрешены, актуальная ER-схема находится в `docs/db_schemas/README.md`.
- [x] **D-01 — завершён:** module, два entrypoint, OpenAPI baseline с implementation status и operational defaults, RFC 9457 problem format, build/lint/unit, Linux race CI, обе container targets, CLI config/help/version и graceful shutdown проверены; stage-0 режим без БД и результат приёмки зафиксированы в `docs/stages/00-contract.md`.
- [x] **D-02 — завершён:** identity/workspace, last-owner race, bootstrap и API/CLI покрыты unit/integration/e2e.
- [x] **D-03 — завершён:** SeaweedFS adapter, migration `000003`, streaming upload/hash, first version, history/download/encoding и DB-conflict compensation проверены.
- [x] **D-04 — завершён:** migration `000004`, idempotent candidate upload, одна active session, encoding-aware bounded diff и rollback warning реализованы.
- [x] **D-05 — завершён:** resolve/reject/expire state machine, idempotent resolve, встроенный scheduler и DB-orphan cleanup реализованы.
- [x] **D-06 — завершён:** migration `000005`, lock/status/unlock, общий files-row guard и конкурентный lock-vs-session test реализованы.
- [x] **D-07 — завершён:** migration/backfill `000006`, automatic current/version links, public/private download и irreversible revoke реализованы.
- [x] **D-08 — минимальный test-ready baseline завершён:** pinned Compose, OpenAPI lint, build/vet/tests, real PostgreSQL/SeaweedFS journey и release/backup instructions готовы; production restore drill и observability отмечены как deployment follow-up.

MVP считается завершённым только после D-08: наличие отдельных работающих endpoint-ов без проверенных транзакционных и отказных сценариев не является готовностью файлового хранилища.
