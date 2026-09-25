---
outline: deep
---

# Installation

PolySchema is written in Go. You can install the `polyschema` command, use the Go library from
your own module, or both. They read the same migration files.

## Requirements

- **Go 1.27.1 or later**, to install the CLI with `go install` or to build against the library
- One of the supported databases:
  - PostgreSQL
  - MySQL
  - MariaDB
  - SQLite (no server needed)

## Command-line tool

```bash
go install github.com/jc21/polyschema/cmd/polyschema@latest
```

This builds `polyschema` into `$(go env GOPATH)/bin` (usually `~/go/bin`). Make sure that folder is
on your `PATH`, then check the install:

```bash
polyschema -h
```

The CLI includes drivers for every supported engine, so you don't need to install anything else:

| Engine | Driver |
|---|---|
| PostgreSQL | [pgx](https://github.com/jackc/pgx) |
| MySQL, MariaDB | [go-sql-driver/mysql](https://github.com/go-sql-driver/mysql) |
| SQLite | [glebarez/go-sqlite](https://github.com/glebarez/go-sqlite) (pure Go, no cgo) |

To pin a version, replace `@latest` with a release tag, for example `@v1.0.0`.

Next: [using the command line](/guide/cli).

## Go library

Add the module to your project:

```bash
go get github.com/jc21/polyschema
```

Then import the `v1` package. It's named `polyschema`, but an explicit alias makes that clear
to readers:

```go
import polyschema "github.com/jc21/polyschema/v1"
```

The library **doesn't import any database drivers**. You open the `*sql.DB` yourself, so add
the driver for your engine as well. Any `database/sql` driver works; these are the ones PolySchema
is tested with:

::: code-group

```bash [PostgreSQL]
go get github.com/jackc/pgx/v5
# import _ "github.com/jackc/pgx/v5/stdlib"   → sql.Open("pgx", dsn)
```

```bash [MySQL / MariaDB]
go get github.com/go-sql-driver/mysql
# import _ "github.com/go-sql-driver/mysql"   → sql.Open("mysql", dsn)
```

```bash [SQLite]
go get github.com/glebarez/go-sqlite
# import _ "github.com/glebarez/go-sqlite"    → sql.Open("sqlite", path)
```

:::

Next: [using the Go library](/guide/library).

::: info Why `/v1`?
The Go module is `github.com/jc21/polyschema`, and `v1/` is a package directory inside it. If the
API ever needs a breaking change, it will go in a new `v2/` package next to it, so code that
imports `/v1` keeps working. Go doesn't allow `/v1` as a *module* suffix, which is why it's a
package directory instead.
:::

## Trying it out locally

To try PolySchema without a database server, use SQLite. The DSN is just a file path:

```bash
polyschema -engine sqlite -dsn ./app.db -dir ./migrations
```

To try the other engines, the repository's `docker-compose.yml` starts throwaway PostgreSQL,
MySQL and MariaDB servers:

```bash
git clone https://github.com/jc21/polyschema.git
cd polyschema
docker compose up -d
```

| Engine | DSN |
|---|---|
| PostgreSQL | `postgres://postgres:test@localhost:5432/postgres?sslmode=disable` |
| MySQL | `root:test@tcp(localhost:3306)/test` |
| MariaDB | `root:test@tcp(localhost:3307)/test` |

These use weak passwords and are meant for local testing only.
