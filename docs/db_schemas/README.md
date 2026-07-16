# FileStore MVP — актуальная ER-схема

Эта Mermaid-схема заменяет историческую иллюстрацию `15-07-mvp.png` как актуальный version-controlled источник архитектуры. Нормативными источниками исполняемой схемы остаются последовательные SQL-файлы в `migrations/`; сущности будущих этапов на диаграмме показывают согласованный target MVP.

```mermaid
erDiagram
    users {
        uuid id PK
        text name
        citext email UK
        text password_hash
        boolean is_superadmin
        timestamptz created_at
        timestamptz updated_at
    }
    user_tokens {
        uuid id PK
        uuid user_id FK
        bytea token_hash UK
        timestamptz expires_at
        timestamptz revoked_at
        timestamptz last_used_at
    }
    workspaces {
        uuid id PK
        text name UK
        text kind
        uuid created_by_user_id FK
        timestamptz created_at
    }
    workspace_members {
        uuid workspace_id PK,FK
        uuid user_id PK,FK
        text role
        timestamptz created_at
    }
    storage_objects {
        uuid id PK
        text object_key UK
        bigint size_bytes
        text sha256
        text mime_type
        timestamptz created_at
    }
    files {
        uuid id PK
        uuid workspace_id FK
        text name
        text text_encoding
        uuid current_version_id FK
        uuid created_by_user_id FK
        timestamptz created_at
        timestamptz updated_at
    }
    file_versions {
        uuid id PK
        uuid file_id FK
        integer version_number
        uuid storage_object_id FK
        text original_name
        uuid created_by_user_id FK
        timestamptz created_at
    }
    file_update_sessions {
        uuid id PK
        uuid file_id FK
        uuid base_version_id FK
        uuid candidate_object_id FK
        text candidate_original_name
        uuid resolved_version_id FK
        text status
        text idempotency_key
        timestamptz expires_at
        uuid created_by_user_id FK
        timestamptz created_at
        timestamptz completed_at
    }
    file_locks {
        uuid id PK
        uuid file_id FK
        text status
        uuid locked_by_user_id FK
        timestamptz created_at
        uuid released_by_user_id FK
        timestamptz released_at
    }
    file_links {
        uuid id PK
        uuid file_id FK
        uuid version_id FK
        text kind
        text token UK
        text status
        uuid created_by_user_id FK
        timestamptz created_at
        timestamptz revoked_at
    }

    users ||--o{ user_tokens : authenticates
    users ||--o{ workspaces : creates
    users ||--o{ workspace_members : participates
    workspaces ||--o{ workspace_members : contains
    workspaces ||--o{ files : contains
    users ||--o{ files : creates
    files ||--|{ file_versions : publishes
    storage_objects ||--o| file_versions : stores
    files ||--o{ file_update_sessions : updates
    file_versions ||--o{ file_update_sessions : bases_or_resolves
    storage_objects ||--o| file_update_sessions : candidate
    files ||--o{ file_locks : locks
    files ||--|{ file_links : exposes
    file_versions ||--|| file_links : version_link
```

Критические ограничения, которые не выражаются одной линией ER:

- ровно один `base`; его UUID — `00000000-0000-0000-0000-000000000001`;
- имена workspace и имена файлов внутри workspace уникальны регистронезависимо;
- `files.current_version_id`, session base/resolved version и version link обязаны ссылаться на версию того же файла;
- у файла не более одной active update session и одной active hard lock; одновременно они существовать не могут;
- private workspace всегда сохраняет хотя бы одного owner;
- `storage_objects.mime_type` обязателен, fallback — `application/octet-stream`;
- таблица блокировок называется `file_locks`, а не `file_update_locks`;
- link token хранится plaintext согласно ADR 0004 и никогда не пишется в логи.
