---
outline: deep
---

# How It Works

## The migration run

When you call `Up` (or run `polyschema` without `-check` or `-dry-run`), PolySchema:

1. **Loads** every migration file at the top level of the folder, validates it, and sorts the
   files by version. Two files with the same version are an error.
2. **Renders** every file to SQL for the target engine. If any operation can't run on that engine,
   it stops here and lists every problem. Nothing has touched the database yet.
3. **Takes a lock** on a single connection (see [Locking](#locking)).
4. **Creates the history table** if it doesn't exist.
5. **Compares** the files against the history table. Applied files are skipped, after their
   [checksum](#checksums) is checked.
6. **Runs each pending file** in version order, inside a transaction unless the file sets
   `transaction: false`, and writes its history row as the last statement.
7. **Releases the lock.**

If a file fails, the run stops there. Files before it stay applied, and the failed file and the
ones after it are applied on the next run.

Migrations only go **forward**; there are no down migrations. To undo a change, write a new
migration that reverses it.

## The history table

By default this is called `schema_migrations` (change it with `WithTable` / `-table`). It has one row
per applied file:

| Column | Type | Contents |
|---|---|---|
| `version` | BIGINT, primary key | The number from the filename |
| `name` | VARCHAR(255) | The filename, such as `10_nickname.yml` |
| `checksum` | VARCHAR(64) | Hex SHA-256 of the file's raw bytes |
| `applied_at` | TIMESTAMP (DATETIME on MySQL/MariaDB, TEXT on SQLite) | When it was applied, in UTC |

This layout is part of the v1 compatibility promise and won't change.

Because files are tracked by **version number**, renaming a file's description (`3_foo.yml` →
`3_bar.yml`) doesn't make it run again, but changing its number does.

## Transactions

Each file runs in one transaction by default, and the history row is inserted in the same
transaction. On Postgres and SQLite, a file is therefore either fully applied and recorded or not
applied at all.

- **MySQL and MariaDB** commit DDL implicitly. If a statement fails after a `CREATE TABLE` or
  `ALTER TABLE` in the same file, that DDL can't be rolled back. PolySchema warns about this for
  every transactional file that contains DDL. Each DDL statement also commits anything that ran
  before it in the same file.
- **`transaction: false`** runs the statements one by one with no transaction. Use it for
  statements that refuse to run inside one, such as Postgres `CREATE INDEX CONCURRENTLY`. If
  such a file fails, the error says explicitly that earlier statements were **not** rolled back.

## Locking

If several instances of your app start at once, they could all try to migrate together. To stop
this, PolySchema takes a session-level advisory lock before it reads the history table:

| Engine | Lock |
|---|---|
| PostgreSQL | `pg_advisory_lock(key)`, where `key` is a 64-bit FNV-1a hash of `polyschema:<table>` |
| MySQL, MariaDB | `GET_LOCK('polyschema:<table>', -1)` |
| SQLite | none |

The first instance gets the lock and migrates. The others wait, then find nothing pending. The lock
is released when the run ends, and also when the connection closes if something goes wrong.

Because the lock name includes the history table name, two apps using different `-table` values
in one database don't block each other.

::: warning SQLite has no lock
Don't run two migrators against the same SQLite file at the same time.
:::

## Checksums

When a file is applied, the SHA-256 of its raw bytes is stored. On later runs, each applied file's
checksum is compared with the file on disk. If they differ:

- **By default**, the run fails:
  ```
  3_add_handle.yml was changed after it was applied (checksum mismatch); pass WithIgnoreChecksums / -ignore-checksums to allow this
  ```
- **With `WithIgnoreChecksums()` / `-ignore-checksums`**, a warning is logged and the run
  continues. The edited file is **not** re-run, and the stored checksum is left as it was.

The opt-out is for the case where you fix an old migration (for example, a mistake that only breaks
on a newly supported engine): new databases get the corrected file, and existing databases keep what
they already ran. The warning appears on every run afterwards, as a reminder that the databases
have diverged.

The checksum covers the whole file, so reformatting, whitespace and comments all count as changes.

## Safety of generated SQL

- **Identifiers are always quoted** (`"users"`, or `` `users` `` on MySQL/MariaDB), with any quote
  characters inside them doubled.
- **Values in `insert`, `update` and `delete` are always bind parameters** (`$1` on Postgres, `?`
  elsewhere), never pasted into the SQL.
- **DDL defaults** can't be bound, so they're inlined as escaped literals.
- **`expr`, `where` on indexes, `defaultExpr` and `sql`** are raw SQL by design, and are passed
  through unchanged. Only put trusted content in them. Migration files are code, so review them like code.

Column names in `insert`, `update` and `where` are sorted, so the same file always produces the
same SQL.

## Versioning

- **Go API:** v1 is the `github.com/jc21/polyschema/v1` package, and its API won't break. A
  redesign would go in a new `v2/` package beside it.
- **File format:** the optional `format:` field is currently `1`, and v1 rejects any other value.
  It lets a future version recognise older files and reject or convert them explicitly.
- **History table:** its layout is part of the v1 contract.
