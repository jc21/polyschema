---
outline: deep
---

# Migration Files

A migration is a YAML or JSON file containing a list of **operations**. PolySchema turns each
operation into SQL for the target engine.

## File names

```
<version>_<description>.yml
```

- `version` is a whole number. Files run in numeric order, so `2_` runs before `10_`. You can
  pad the numbers (`0002_`) or use timestamps (`20260925_`) if you prefer. Each version must
  be unique.
- The description can be separated by `_` or `-`, and can be left out.
- The extension must be `.yml`, `.yaml` or `.json`.

Files that don't match this pattern are ignored when loading a folder, so a `README.md` next to
your migrations is fine. Subfolders are ignored too.

```
migrations/
├── 1_create_roles_users.yml
├── 2_seed_roles.json
└── 10_nickname.yml
```

::: warning Don't edit applied files
Once a file has been applied anywhere, treat it as frozen and make changes in a new file. PolySchema stores each
file's checksum and fails if an applied file changes. See [Checksums](/guide/how-it-works#checksums).
:::

## File structure

```yaml
format: 1          # optional. The file-format version; only 1 is accepted.
transaction: true  # optional, default true. Run the whole file in one transaction.
operations:        # required, at least one
  - createTable: { ... }
  - insert: { ... }
```

Each item in `operations` has **exactly one** operation key. Unknown keys and misspelled field
names are errors, so a typo like `nulable: false` is caught rather than ignored.

JSON works the same way:

```json
{
	"operations": [
		{ "insert": { "table": "roles", "rows": [ { "id": 1, "name": "admin" }, { "id": 2, "name": "user" } ] } }
	]
}
```

## Columns

Columns are used by `createTable`, `addColumn` and `alterColumn`.

```yaml
- { name: email, type: string, length: 255, nullable: false, unique: true }
```

| Field | Description |
|---|---|
| `name` | Required. |
| `type` | Required. One of the [portable types](/guide/engines#types): `smallint`, `integer`, `bigint`, `string`, `text`, `boolean`, `decimal`, `float`, `double`, `date`, `time`, `timestamp`, `timestamptz`, `json`, `uuid`, `binary`. |
| `length` | For `string`. Defaults to `255`. |
| `precision`, `scale` | For `decimal`. `precision` is required and must be greater than 0; `scale` defaults to 0 and can't be greater than `precision`. |
| `nullable` | Columns are nullable unless this is `false` or the column is part of the primary key. |
| `primaryKey` | Make this column (part of) the primary key. |
| `autoIncrement` | Only allowed on an integer column that is the table's **only** primary-key column. Can't have a default. |
| `unique` | Add an inline `UNIQUE` constraint. |
| `default` | A literal string, number or boolean. Quote dates: `default: "2026-01-01"`. MySQL doesn't allow a literal default on `text`, `json` or `binary` columns; use `defaultExpr` there. |
| `defaultExpr` | A raw SQL expression. `CURRENT_TIMESTAMP`, `CURRENT_DATE` and `CURRENT_TIME` are written as-is; anything else is wrapped in parentheses. |

`default` and `defaultExpr` can't both be set.

## Table operations

### createTable

```yaml
- createTable:
    name: users
    ifNotExists: true
    columns:
      - { name: id,         type: bigint,    primaryKey: true, autoIncrement: true }
      - { name: email,      type: string,    length: 255, nullable: false }
      - { name: role_id,    type: integer }
      - { name: active,     type: boolean,   nullable: false, default: true }
      - { name: created_at, type: timestamp, defaultExpr: CURRENT_TIMESTAMP }
    uniques:     [{ name: uq_users_email, columns: [email] }]
    indexes:     [{ name: idx_users_role, columns: [role_id] }]
    foreignKeys: [{ name: fk_users_role, columns: [role_id], refTable: roles, refColumns: [id], onDelete: set null }]
    checks:      [{ name: chk_email, expr: "email <> ''" }]
```

| Field | Description |
|---|---|
| `name` | Required. |
| `ifNotExists` | Skip the table if it already exists. On Postgres and SQLite, its indexes get `IF NOT EXISTS` as well. |
| `columns` | Required. A list of [columns](#columns). |
| `primaryKey` | A list of column names, for a composite key. Use either this or `primaryKey: true` on columns, not both. |
| `uniques` | A list of `{ name, columns }`. |
| `indexes` | A list of `{ name, columns, unique, where }`; see [createIndex](#createindex). |
| `foreignKeys` | A list of `{ name, columns, refTable, refColumns, onDelete, onUpdate }`; see [addForeignKey](#addforeignkey-dropforeignkey). |
| `checks` | A list of `{ name, expr }`; see [addCheck](#addcheck-dropcheck). |

You don't need to give a `table` on nested indexes, uniques, foreign keys or checks; they
use the table being created.

### dropTable

```yaml
- dropTable: { name: legacy, ifExists: true, cascade: true }
```

`cascade` also drops objects that depend on the table. Only Postgres supports it; the other
engines ignore it with a warning.

### renameTable

```yaml
- renameTable: { from: people, to: persons }
```

## Column operations

### addColumn

```yaml
- addColumn:
    table: users
    ifNotExists: true
    column: { name: nickname, type: string, length: 50 }
```

A new column can't be `primaryKey` or `autoIncrement`. `ifNotExists` works on Postgres and MariaDB only.
SQLite can't add a column that is `unique`, `NOT NULL` without a default, or has a `defaultExpr`.

### dropColumn

```yaml
- dropColumn: { table: users, name: nickname, ifExists: true }
```

`ifExists` works on Postgres and MariaDB only.

### renameColumn

```yaml
- renameColumn: { table: users, from: nickname, to: handle }
```

### alterColumn

```yaml
- alterColumn: { table: users, name: handle, type: string, length: 100, nullable: false, default: "" }
```

`alterColumn` **restates the whole column**, using the same fields as a [column](#columns). The type,
nullability and default all end up exactly as written, so if you leave out `default`, the existing
default is dropped. `primaryKey`, `autoIncrement` and `unique` can't be changed here; use
[addUnique](#addunique-dropunique) instead.

SQLite doesn't support `alterColumn`.

## Indexes and constraints

### createIndex

```yaml
- createIndex:
    table: users
    name: idx_users_handle
    columns: [handle]
    unique: true
    ifNotExists: true
    where: "active"        # partial index predicate, raw SQL
```

`where` (a partial index) works on Postgres and SQLite only. `ifNotExists` works everywhere except
MySQL. The same goes for `ifExists` on [dropIndex](#dropindex).

### dropIndex

```yaml
- dropIndex: { table: users, name: idx_users_handle, ifExists: true }
```

`table` is required, because MySQL needs it.

### addUnique / dropUnique

```yaml
- addUnique:  { table: users, name: uq_users_handle, columns: [handle] }
- dropUnique: { table: users, name: uq_users_handle }
```

On SQLite, these create and drop a unique **index** instead, with a warning.

### addForeignKey / dropForeignKey

```yaml
- addForeignKey:
    table: users
    name: fk_users_role
    columns: [role_id]
    refTable: roles
    refColumns: [id]
    onDelete: cascade
    onUpdate: restrict
- dropForeignKey: { table: users, name: fk_users_role }
```

`columns` and `refColumns` must be the same length. `onDelete` and `onUpdate` accept `cascade`,
`restrict`, `set null`, `set default` or `no action`.

SQLite can only declare foreign keys in `createTable`.

### addCheck / dropCheck

```yaml
- addCheck:  { table: users, name: chk_email_len, expr: "length(email) > 3" }
- dropCheck: { table: users, name: chk_email_len }
```

`expr` is raw SQL and is passed through unchanged, so keep it portable if you target several
engines. SQLite can only declare checks in `createTable`.

## Data operations

Values in data operations are always sent as bind parameters, never pasted into the SQL. A
nested map or list is stored as JSON text, which suits `json` columns.

### insert

```yaml
- insert:
    table: roles
    rows:
      - { id: 1, name: admin }
      - { id: 2, name: user, settings: { theme: dark } }
```

Each row becomes its own `INSERT` statement, so rows can have different columns.

### update

```yaml
- update: { table: users, set: { active: false }, where: { role_id: 2 } }
- update: { table: users, set: { active: true }, all: true }
```

### delete

```yaml
- delete: { table: users, where: { active: false, deleted_at: null } }
```

`update` and `delete` need **either** a `where` **or** `all: true`, never both. That way you can't
accidentally change every row. Every `where` key must match (they're combined with `AND`), and
`null` means `IS NULL`.

For anything more complex, such as ranges, `OR`, joins or subqueries, use [sql](#sql).

## sql

Runs raw SQL. Give a statement per engine, a `default`, or both:

```yaml
- sql:
    postgres: "CREATE EXTENSION IF NOT EXISTS citext"
    mysql:    "ALTER TABLE users CONVERT TO CHARACTER SET utf8mb4"
    default:  "SELECT 1"
```

PolySchema picks the entry for the target engine. MariaDB falls back to the `mysql` entry, and
any engine falls back to `default`. If an engine has no entry and there's no `default`, the file
is refused on that engine, and `-check` reports it.

The accepted keys are `postgres`, `mysql`, `mariadb`, `sqlite` and `default`. Use `sql` for anything
the portable operations don't cover, such as SQLite table rebuilds, views, triggers or engine-specific
tuning.

## Transactions

Each file runs in a transaction by default, and the history row is written in the same
transaction, so a file is either fully applied and recorded, or not applied at all.

Some statements can't run inside a transaction, such as Postgres `CREATE INDEX CONCURRENTLY`. For
those, turn it off for the file:

```yaml
transaction: false
operations:
  - sql:
      postgres: "CREATE INDEX CONCURRENTLY idx_orders_created ON orders (created_at)"
      default:  "CREATE INDEX idx_orders_created ON orders (created_at)"
```

If a non-transactional file fails partway through, the statements that already ran stay applied.
The error says so, and you'll have to clean up by hand before retrying. Keep such files short.

::: info MySQL and MariaDB
These engines commit DDL implicitly, so a transaction can't undo a `CREATE TABLE` or `ALTER TABLE`
that has already run. PolySchema warns about this on every transactional file that contains DDL.
Keeping one DDL change per file makes failures easier to recover from on those engines.
:::

## A complete example

```yaml
# migrations/1_create_roles_users.yml
operations:
  - createTable:
      name: roles
      ifNotExists: true
      columns:
        - { name: id,   type: integer, primaryKey: true }
        - { name: name, type: string, length: 50, nullable: false, unique: true }

  - createTable:
      name: users
      columns:
        - { name: id,         type: bigint,  primaryKey: true, autoIncrement: true }
        - { name: email,      type: string,  length: 255, nullable: false }
        - { name: role_id,    type: integer }
        - { name: active,     type: boolean, nullable: false, default: true }
        - { name: balance,    type: decimal, precision: 10, scale: 2, default: 0 }
        - { name: settings,   type: json }
        - { name: created_at, type: timestamp, nullable: false, defaultExpr: CURRENT_TIMESTAMP }
      uniques:
        - { name: uq_users_email, columns: [email] }
      indexes:
        - { name: idx_users_role, columns: [role_id] }
      foreignKeys:
        - { name: fk_users_role, columns: [role_id], refTable: roles, refColumns: [id], onDelete: set null }
      checks:
        - { name: chk_users_email, expr: "email <> ''" }
```

```jsonc
// migrations/2_seed_roles.json
{
	"operations": [
		{ "insert": { "table": "roles", "rows": [ { "id": 1, "name": "admin" }, { "id": 2, "name": "user" } ] } },
		{ "insert": { "table": "users", "rows": [ { "email": "admin@example.com", "role_id": 1, "settings": { "theme": "dark" } } ] } }
	]
}
```

```yaml
# migrations/10_nickname.yml
operations:
  - addColumn:    { table: users, column: { name: nickname, type: string, length: 50 } }
  - renameColumn: { table: users, from: nickname, to: handle }
  - createIndex:  { table: users, name: idx_users_handle, columns: [handle] }
  - update:       { table: users, set: { handle: boss }, where: { role_id: 1 } }
  - delete:       { table: users, where: { active: false } }
  - sql:
      postgres: "COMMENT ON TABLE users IS 'app users'"
      default:  "SELECT 1"
```
