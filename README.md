<p align="center">
    <img src="https://polyschema.jc21.com/images/favicon/apple-touch-icon.png">
</p>

# PolySchema

Write each migration once, in YAML or JSON, and apply it to **PostgreSQL, MySQL, MariaDB or SQLite**. You can use it as a Go library (it works with `embed.FS`) or as a standalone command.

- Migrations only go forward. They run in numeric filename order (`2_` before `10_`), and each one is recorded in a history table.
- Each file runs in a transaction by default (`transaction: false` opts out).
- A Postgres/MySQL advisory lock stops app instances that start at the same time from migrating twice.
- `-check` reports anything a given engine can't do before you run it.

## Library

```go
import polyschema "github.com/jc21/polyschema/v1"

//go:embed migrations
var migrations embed.FS

sub, _ := fs.Sub(migrations, "migrations")
m := polyschema.New(db, polyschema.Postgres, // or MySQL, MariaDB, SQLite
	polyschema.WithLogger(slog.Default()))
if err := m.Up(ctx, sub); err != nil { ... }
```

You open the `*sql.DB` yourself with whichever driver you like; the library doesn't import any drivers.

| Option | Default | |
|---|---|---|
| `WithTable(name)` | `schema_migrations` | name of the history table |
| `WithLogger(l)` | discarded | progress messages and warnings |
| `WithIgnoreChecksums()` | off | an applied file that was edited later logs a warning instead of failing; it is not re-run |

Other entry points: `m.ApplyFile(ctx, fsys, name)`, `Load(fsys)`, `Parse(name, data)`, `Render(file, dialect)` (returns the SQL without running it), `Check(files, dialects...)` and `CheckFS(fsys, dialects...)`.

## Command

```bash
go install github.com/jc21/polyschema/cmd/polyschema@latest
```

```
polyschema -engine postgres -dsn "$DSN" -dir ./migrations      # apply pending
polyschema -engine sqlite -dsn app.db -file ./migrations/3_x.yml
polyschema -check -dir ./migrations                  # every engine, no DB needed
polyschema -check -engine mysql -strict -dir ./migrations
polyschema -dry-run -engine mysql -dir ./migrations  # print the SQL
```

Other flags: `-ignore-checksums` and `-table`. `-dsn` falls back to `$POLYSCHEMA_DSN`, which keeps passwords out of your shell history. Exit codes: 0 = ok, 1 = failed or `-check` found errors (with `-strict`, warnings count too), 2 = bad usage.

DSN examples:
- postgres: `postgres://user:pass@host:5432/db?sslmode=disable`
- mysql/mariadb: `user:pass@tcp(host:3306)/db`
- sqlite: a file path

## Migration files

The name is `<number>_<description>.yml`, `.yaml` or `.json`. The number is the version and must be unique. Files with other names are ignored.

```yaml
format: 1          # optional; file-format version
transaction: true  # optional; default true
operations:
  - createTable:
      name: users
      ifNotExists: true
      columns:
        - { name: id,         type: bigint,  primaryKey: true, autoIncrement: true }
        - { name: email,      type: string,  length: 255, nullable: false }
        - { name: role_id,    type: integer }
        - { name: active,     type: boolean, nullable: false, default: true }
        - { name: created_at, type: timestamp, defaultExpr: CURRENT_TIMESTAMP }
      primaryKey: [id]     # alternative to column-level primaryKey, for composite keys
      uniques:     [{ name: uq_users_email, columns: [email] }]
      indexes:     [{ name: idx_users_role, columns: [role_id] }]
      foreignKeys: [{ name: fk_users_role, columns: [role_id], refTable: roles, refColumns: [id], onDelete: set null }]
      checks:      [{ name: chk_email, expr: "email <> ''" }]

  - dropTable:      { name: legacy, ifExists: true, cascade: true }
  - renameTable:    { from: people, to: persons }
  - addColumn:      { table: users, ifNotExists: true, column: { name: nick, type: string, length: 50 } }
  - dropColumn:     { table: users, name: nick, ifExists: true }
  - renameColumn:   { table: users, from: nick, to: handle }
  - alterColumn:    { table: users, name: handle, type: string, length: 100, nullable: false, default: "" }
  - createIndex:    { table: users, name: idx_handle, columns: [handle], unique: true, ifNotExists: true, where: "active" }
  - dropIndex:      { table: users, name: idx_handle, ifExists: true }
  - addUnique:      { table: users, name: uq_handle, columns: [handle] }
  - dropUnique:     { table: users, name: uq_handle }
  - addForeignKey:  { table: users, name: fk_x, columns: [role_id], refTable: roles, refColumns: [id], onDelete: cascade, onUpdate: restrict }
  - dropForeignKey: { table: users, name: fk_x }
  - addCheck:       { table: users, name: chk_x, expr: "length(email) > 3" }
  - dropCheck:      { table: users, name: chk_x }

  - insert: { table: roles, rows: [{ id: 1, name: admin }, { id: 2, name: user }] }
  - update: { table: users, set: { active: false }, where: { role_id: 2 } }
  - delete: { table: users, where: { active: false } }

  - sql:  # raw SQL for anything else; uses the engine's entry, otherwise `default`
      postgres: "CREATE EXTENSION IF NOT EXISTS citext"
      default:  "SELECT 1"
```

How some of the fields behave:
- **Columns** are nullable unless `nullable: false` is set or they're part of the primary key. `string` defaults to length 255. `decimal` needs a `precision` (and optionally a `scale`).
- **`autoIncrement`** is only allowed on an integer column that is the table's only primary-key column.
- **`default`** is a literal value. **`defaultExpr`** is raw SQL: `CURRENT_TIMESTAMP`, `CURRENT_DATE` and `CURRENT_TIME` are written as-is, and anything else is wrapped in parentheses.
- **`alterColumn`** restates the whole column. The type, nullability and default all end up exactly as written, so leaving out `default` drops the existing one.
- **`update` and `delete`** need a `where` (every key must match, and `null` means `IS NULL`) or `all: true`. Values are always passed as bind parameters. A nested map or list is stored as JSON text.
- **`expr` and `where`** strings (checks and partial indexes) are raw SQL that's passed through unchanged.

### Types

| type | postgres | mysql / mariadb | sqlite |
|---|---|---|---|
| smallint / integer / bigint | SMALLINT / INTEGER / BIGINT | same | INTEGER |
| + autoIncrement | GENERATED BY DEFAULT AS IDENTITY | AUTO_INCREMENT | INTEGER PRIMARY KEY AUTOINCREMENT |
| string | VARCHAR(n) | VARCHAR(n) | TEXT |
| text | TEXT | TEXT | TEXT |
| boolean | BOOLEAN | TINYINT(1) | INTEGER |
| decimal | NUMERIC(p,s) | DECIMAL(p,s) | NUMERIC |
| float / double | REAL / DOUBLE PRECISION | FLOAT / DOUBLE | REAL |
| date / time / timestamp | DATE / TIME / TIMESTAMP | DATE / TIME / DATETIME | TEXT |
| timestamptz | TIMESTAMPTZ | DATETIME ⚠ | TEXT |
| json | JSONB | JSON | TEXT |
| uuid | UUID | CHAR(36) | TEXT |
| binary | BYTEA | BLOB | BLOB |

### What each engine can't do

`-check` reports these. A **warning** means the migration still runs, but with a weaker result. An **error** means the migration is refused on that engine.

| | MySQL | MariaDB | SQLite |
|---|---|---|---|
| DDL inside a transaction | ⚠ commits implicitly | ⚠ commits implicitly | ✓ |
| `dropTable cascade` | ⚠ ignored | ⚠ ignored | ⚠ ignored |
| `createIndex/dropIndex/dropColumn ifExists/ifNotExists` | ✗ | ✓ | ✓ index, ✗ column |
| `addColumn ifNotExists` | ✗ | ✓ | ✗ |
| partial index (`where`) | ✗ | ✗ | ✓ |
| `timestamptz` | ⚠ DATETIME | ⚠ DATETIME | ✓ (TEXT) |
| literal default on text/json/binary | ✗ use `defaultExpr` | ✓ | ✓ |
| `alterColumn`, `add/dropForeignKey`, `add/dropCheck` | ✓ | ✓ | ✗ needs a table rebuild |
| `add/dropUnique` | ✓ | ✓ | ⚠ done with a unique index |
| `addColumn` that is unique, NOT NULL with no default, or has an expression default | ✓ | ✓ | ✗ |

Postgres supports everything in the format. On SQLite, foreign keys are only enforced if you turn on `PRAGMA foreign_keys = ON` for your connection.

### Concurrency and checksums

`Up` holds `pg_advisory_lock` or MySQL's `GET_LOCK` while it runs. SQLite gets no lock, so don't run two migrators against the same SQLite file at once.

Each applied file's SHA-256 checksum is stored. If an applied file is later edited, `Up` fails unless you pass `WithIgnoreChecksums` / `-ignore-checksums`. With that option, new databases get the edited version and existing ones keep what they already ran.

## Versioning

- **Go API:** v1 lives in the `v1/` package. A breaking redesign would go in a new `v2/` package beside it, so projects importing `/v1` never change. (Go won't allow `/v1` as a *module* suffix, which is why it's a package directory instead.)
- **File format:** the optional `format:` field (currently `1`) lets a future package reject or convert older files explicitly.
- **History table:** its layout (`version, name, checksum, applied_at`) is part of the v1 contract.

## Development

```bash
go test -race -cover ./...           # SQLite plus unit tests; no servers needed
go test ./v1 -run Golden -update     # regenerate golden SQL after generator changes
docker compose up -d                 # then set the DSNs listed in docker-compose.yml
```
