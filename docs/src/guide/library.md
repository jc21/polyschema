---
outline: deep
---

# Go Library

The `polyschema` package runs migrations from your own Go program, which usually means at startup,
so the app always runs against the schema it expects. See [Installation](/setup/#go-library) to add it
to your module.

```go
import polyschema "github.com/jc21/polyschema/v1"
```

## Basic usage

Embed your migrations folder in the binary, open a `*sql.DB` with the driver of your choice, and
call `Up`:

```go
package main

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"
	"log"
	"log/slog"

	_ "github.com/jackc/pgx/v5/stdlib"
	polyschema "github.com/jc21/polyschema/v1"
)

//go:embed migrations/*
var migrations embed.FS

func main() {
	ctx := context.Background()

	db, err := sql.Open("pgx", "postgres://user:pass@localhost:5432/app?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Migration files must be at the top level of the FS you pass in.
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		log.Fatal(err)
	}

	m := polyschema.New(db, polyschema.Postgres, polyschema.WithLogger(slog.Default()))
	if err := m.Up(ctx, sub); err != nil {
		log.Fatalf("migrating database: %v", err)
	}

	// ... start the app
}
```

`Up` applies every pending file in version order and does nothing if they're all applied, so it's
safe to call on every start. On Postgres and MySQL/MariaDB, an advisory lock makes other instances
wait until the first one has finished; they then find nothing left to do.

`Up` takes any `fs.FS`, so `os.DirFS("./migrations")` works just as well as an `embed.FS`.

## Choosing an engine

Pass one of the dialect values to `New`:

| Dialect | Engine | Common driver (`sql.Open` name) |
|---|---|---|
| `polyschema.Postgres` | PostgreSQL | `github.com/jackc/pgx/v5/stdlib` (`"pgx"`) |
| `polyschema.MySQL` | MySQL | `github.com/go-sql-driver/mysql` (`"mysql"`) |
| `polyschema.MariaDB` | MariaDB | `github.com/go-sql-driver/mysql` (`"mysql"`) |
| `polyschema.SQLite` | SQLite | `github.com/glebarez/go-sqlite` (`"sqlite"`) |

Choose `MariaDB` rather than `MySQL` when you're on MariaDB, even though they use the same driver.
MariaDB supports a few things MySQL doesn't, such as `IF [NOT] EXISTS` on indexes and columns.

If the engine comes from configuration, look it up by name. `DialectByName` ignores case and
returns `nil` for an unknown name:

```go
d := polyschema.DialectByName(cfg.DBEngine) // "postgres", "mysql", "mariadb" or "sqlite"
if d == nil {
	return fmt.Errorf("unsupported database engine %q", cfg.DBEngine)
}
m := polyschema.New(db, d)
```

`polyschema.All` lists all four dialects, which is handy for [`Check`](#checking-migrations-in-tests).

::: tip SQLite
Use a file-backed database. With `:memory:`, each connection in the `*sql.DB` pool gets its own
empty database. Also note that SQLite only enforces foreign keys when `PRAGMA foreign_keys = ON` is
set on the connection.
:::

## Options

Pass any of these to `New`:

| Option | Default | Description |
|---|---|---|
| `WithTable(name)` | `schema_migrations` | Name of the history table. |
| `WithLogger(l *slog.Logger)` | discarded | Where progress messages and warnings go. |
| `WithIgnoreChecksums()` | off | If an applied file has been edited since, log a warning instead of failing. The file is not re-run. See [Checksums](/guide/how-it-works#checksums). |

```go
m := polyschema.New(db, polyschema.MySQL,
	polyschema.WithTable("billing_migrations"),
	polyschema.WithLogger(logger.With("component", "migrations")),
)
```

::: warning Set a logger
Without `WithLogger`, warnings are discarded too. That includes engine warnings (such as MySQL
committing DDL implicitly) and checksum mismatches that `WithIgnoreChecksums` lets through.
:::

## Applying one file

`ApplyFile` applies a single named file, unless it has already been applied:

```go
err := m.ApplyFile(ctx, sub, "7_backfill_handles.yml")
```

## Errors

`Up` and `ApplyFile` return an error when:

- a file can't be read, parsed or validated, two files share a version, or a filename isn't a valid
  migration name (`Load` skips files that don't look like migrations, but `ApplyFile` doesn't)
- any file uses an operation the engine can't run. Every file is rendered **before** the database
  is touched, so this fails before anything is applied, and each problem is listed.
- an applied file's checksum no longer matches (unless `WithIgnoreChecksums` is set)
- a statement fails. The error names the file, the operation number and the SQL:

  ```
  4_seed.yml: operation 2: SQL logic error: no such table: user (1)
    sql: INSERT INTO "user" ("email") VALUES (?)
  ```

  The file's transaction is rolled back, and files before it stay applied. If the file has
  `transaction: false`, the error says so explicitly, because the earlier statements in that
  file were **not** rolled back:

  ```
    (transaction: false, so earlier statements in this file were NOT rolled back)
  ```

Pass a context with a deadline or cancellation to bound how long `Up` waits for the lock and
runs statements.

## Checking migrations in tests

`CheckFS` renders every file for the dialects you give it and returns the problems, without
needing a database. Put it in a test and CI will catch a migration that won't run on one of
your engines:

```go
func TestMigrations(t *testing.T) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	issues, err := polyschema.CheckFS(sub, polyschema.All...)
	if err != nil {
		t.Fatal(err) // a file failed to parse or validate
	}
	for _, is := range issues {
		if is.Severity == polyschema.Error {
			t.Error(is)
		} else {
			t.Log(is)
		}
	}
}
```

Only check the engines you actually support, for example
`polyschema.CheckFS(sub, polyschema.Postgres, polyschema.SQLite)`.

Each `Issue` has `File`, `Op` (1-based operation index; 0 means the whole file), `Engine`,
`Severity` (`polyschema.Warning` or `polyschema.Error`) and `Message`. `Issue.String()` formats
it the same way as the CLI does.

## Rendering SQL without running it

`Render` turns one parsed file into SQL for a dialect. It's what `polyschema -dry-run` uses:

```go
files, err := polyschema.Load(sub)
if err != nil {
	return err
}
for _, f := range files {
	stmts, issues := polyschema.Render(f, polyschema.MySQL)
	for _, is := range issues {
		fmt.Println("--", is)
	}
	for _, s := range stmts {
		fmt.Printf("%s; -- args: %v\n", s.SQL, s.Args)
	}
}
```

Each `Statement` has `Op` (the operation it came from), `SQL` and `Args` (bind arguments). Don't
run the statements if any issue has `Severity == polyschema.Error`.

## API reference

| Function | Description |
|---|---|
| `New(db, dialect, opts...) *Migrator` | Create a migrator for one database. |
| `(*Migrator) Up(ctx, fsys) error` | Apply every pending migration in `fsys`. |
| `(*Migrator) ApplyFile(ctx, fsys, name) error` | Apply one file unless it's already applied. |
| `Load(fsys) ([]*File, error)` | Read and validate every migration at the top level of `fsys`, sorted by version. |
| `LoadFile(fsys, name) (*File, error)` | Read and validate one file. |
| `Parse(name, data) (*File, error)` | Parse migration bytes. `name` supplies the version, so it must be a valid migration filename. |
| `Render(file, dialect) ([]Statement, []Issue)` | Generate SQL for one engine. |
| `Check(files, dialects...) []Issue` | Render loaded files for each dialect and collect the issues. |
| `CheckFS(fsys, dialects...) ([]Issue, error)` | `Load` followed by `Check`. |
| `DialectByName(name) *Dialect` | Look up `postgres`, `mysql`, `mariadb` or `sqlite`, or return `nil`. |

A parsed `File` exposes `Name`, `Version`, `Checksum` (hex SHA-256 of the raw file), `Format` and
`Operations`, plus `UseTransaction()`. The operation structs mirror the
[file format](/guide/migrations) field for field.

Full Go documentation is on [pkg.go.dev](https://pkg.go.dev/github.com/jc21/polyschema/v1).
