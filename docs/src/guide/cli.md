---
outline: deep
---

# Command Line

The `polyschema` command applies, checks or prints migration files. See
[Installation](/setup/) to install it.

```bash
polyschema [flags]
```

Every command needs **exactly one** of `-dir` (a folder of migrations) or `-file` (a single migration
file).

## Applying migrations

Pass `-engine`, a DSN and your migrations folder:

```bash
polyschema -engine postgres -dsn "postgres://user:pass@localhost:5432/app?sslmode=disable" -dir ./migrations
```

PolySchema then:

1. reads and validates every file, and renders it for the engine. If any file uses something the engine
   can't do, it stops here, before connecting.
2. takes the migration lock (Postgres and MySQL/MariaDB only)
3. creates the history table if it doesn't exist
4. runs each file that isn't in the history table yet, in version order, and records it

Progress is logged to stderr:

```
time=2026-09-25T12:50:51.956+10:00 level=INFO msg="applying migration" file=1_create_roles.yml engine=postgres
time=2026-09-25T12:50:51.957+10:00 level=INFO msg="applying migration" file=2_seed_roles.json engine=postgres
```

It's safe to run again. Files that have already been applied are skipped, so running it when
nothing is pending does nothing and exits 0.

### A single file

`-file` applies one file, unless it has already been applied:

```bash
polyschema -engine sqlite -dsn ./app.db -file ./migrations/3_add_nickname.yml
```

### Keeping the password out of your shell history

If you leave out `-dsn`, it falls back to the `POLYSCHEMA_DSN` environment variable:

```bash
export POLYSCHEMA_DSN="user:pass@tcp(db.internal:3306)/app"
polyschema -engine mysql -dir ./migrations
```

### DSN formats

| Engine | Example DSN |
|---|---|
| `postgres` | `postgres://user:pass@host:5432/db?sslmode=disable` |
| `mysql`, `mariadb` | `user:pass@tcp(host:3306)/db` |
| `sqlite` | a file path, such as `./app.db` |

The Postgres DSN is passed to [pgx](https://pkg.go.dev/github.com/jackc/pgx/v5/stdlib), and the MySQL/MariaDB
DSN to [go-sql-driver/mysql](https://github.com/go-sql-driver/mysql#dsn-data-source-name), so any
option those drivers accept works here too.

## Checking migrations

`-check` renders every file for every engine and reports anything an engine can't do. It doesn't
need a database or a DSN.

```bash
polyschema -check -dir ./migrations
```

```
1_create_users.yml:0 mysql warning: mysql commits DDL implicitly, so a failed migration can't be fully rolled back
1_create_users.yml:0 mariadb warning: mariadb commits DDL implicitly, so a failed migration can't be fully rolled back
2_active_idx.yml:2 mysql error: mysql has no partial indexes (index idx_users_active has where)
2_active_idx.yml:2 mariadb error: mariadb has no partial indexes (index idx_users_active has where)
```

Each line reads `file:operation engine severity: message`. The operation number starts at 1, and
`0` means the whole file.

- A **warning** means the migration still runs on that engine, but the result is weaker. For
  example, `cascade` is ignored.
- An **error** means the migration will be refused on that engine.

Check against a single engine with `-engine`, and add `-strict` to treat warnings as failures too:

```bash
polyschema -check -engine mysql -strict -dir ./migrations
```

`-check` also catches structural mistakes, such as a misspelled field, an unknown type or an
`update` without a `where`. It's worth running in CI on every change to your migrations:

```yaml
# .github/workflows/migrations.yml (excerpt)
- run: go install github.com/jc21/polyschema/cmd/polyschema@latest
- run: polyschema -check -dir ./migrations
```

See [Types & Engine Support](/guide/engines#what-each-engine-can-t-do) for the full list of what
gets reported.

## Previewing the SQL

`-dry-run` prints the SQL for one engine without connecting to anything. Bind arguments are shown
in a comment after each statement that has them:

```bash
polyschema -dry-run -engine sqlite -dir ./migrations
```

```sql
-- 1_create_roles.yml
CREATE TABLE IF NOT EXISTS "roles" (
  "id" INTEGER NOT NULL,
  "name" TEXT NOT NULL UNIQUE,
  PRIMARY KEY ("id")
);
-- 2_seed_roles.json
INSERT INTO "roles" ("id", "name") VALUES (?, ?);
-- args: [1 admin]
INSERT INTO "roles" ("id", "name") VALUES (?, ?);
-- args: [2 user]
```

The SQL goes to stdout and any issues go to stderr, so you can redirect the SQL to a file
(`> plan.sql`) and still see the warnings.

## Flags

| Flag | Default | Description |
|---|---|---|
| `-dir <path>` | | Folder of migration files. Only the top level is read. |
| `-file <path>` | | A single migration file. |
| `-engine <name>` | | `postgres`, `mysql`, `mariadb` or `sqlite`. Required to apply migrations or with `-dry-run`. With `-check`, limits the check to that engine. |
| `-dsn <string>` | `$POLYSCHEMA_DSN` | Connection string. Required to apply migrations. |
| `-check` | off | Report engine support issues instead of applying. Checks every engine unless `-engine` is set. |
| `-strict` | off | With `-check`, warnings fail too. |
| `-dry-run` | off | Print the SQL for `-engine` instead of running it. |
| `-ignore-checksums` | off | If an applied file has been edited since, log a warning instead of failing. The file is not re-run. See [Checksums](/guide/how-it-works#checksums). |
| `-table <name>` | `schema_migrations` | Name of the migration history table. |

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success. With `-check`, no errors were found (and no warnings, with `-strict`). |
| `1` | A migration failed, a file couldn't be read or parsed, or `-check` found errors (or warnings, with `-strict`). |
| `2` | Bad usage, such as an unknown flag, a missing `-dir`/`-file`, an unknown engine or a missing DSN. |

## Recipes

**Run migrations before starting your app (Docker entrypoint):**

```bash
#!/bin/sh
set -e
polyschema -engine postgres -dir /app/migrations   # DSN from $POLYSCHEMA_DSN
exec /app/server
```

The advisory lock means this is safe even when several replicas start at once.

**Use a custom history table** (for example, when two apps share one database):

```bash
polyschema -engine mysql -table billing_migrations -dir ./migrations
```

**Diff the SQL across engines:**

```bash
for e in postgres mysql mariadb sqlite; do
  polyschema -dry-run -engine "$e" -dir ./migrations > "plan.$e.sql"
done
```
