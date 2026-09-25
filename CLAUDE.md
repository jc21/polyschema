# CLAUDE.md

PolySchema: portable YAML/JSON migrations applied to Postgres, MySQL, MariaDB and SQLite. It ships as both a library and a CLI. User-facing docs (file format, type table, engine gaps) live in README.md. Keep README in sync whenever behaviour changes.

## Layout
- `v1/spec.go`: file/operation structs, `Parse`, and all structural validation.
- `v1/dialect.go`: `Dialect` vars, identifier quoting, placeholders, default literals, and the `typeNames` map.
- `v1/generate.go`: `Render(file, dialect) ([]Statement, []Issue)`, `Check`, `CheckFS`. All per-engine SQL lives here.
- `v1/migrator.go`: `Load`/`LoadFile`, `Migrator` (`Up`, `ApplyFile`), history table, lock, transactions.
- `cmd/polyschema/main.go`: a thin CLI. `run(args, stdout, stderr) int` exists so it can be tested.

## Commands
```bash
go test -race -cover ./...               # unit + real SQLite; no servers needed
go test ./v1 -run Golden -update         # regenerate testdata/golden after generator changes
go run ./cmd/polyschema -check -dir v1/testdata/migrations
go run ./cmd/polyschema -dry-run -engine mysql -dir v1/testdata/migrations
```
- Integration tests run against real servers when `POLYSCHEMA_PG_DSN`, `POLYSCHEMA_MYSQL_DSN` or `POLYSCHEMA_MARIA_DSN` is set; `docker-compose.yml` has example DSNs. Without them, only SQLite runs, and the Postgres/MySQL advisory lock code (`lock()`) is uncovered.
- Docker was not available on the dev machine when this was built, so the Postgres, MySQL and MariaDB paths have never been run against real servers. Do that before trusting them.

## Decisions (agreed with the user; don't reverse without asking)
- **Versioning:** Go rejects module paths ending in `/v1` ("major version suffixes ... only allowed for v2 or later"; this was verified). So the module is `github.com/jc21/polyschema`, and `v1/` is a *package directory* named `polyschema`, the same pattern as `k8s.io/api/core/v1`. A future `v2/` package goes next to it. v1's API must never break, because both share the same git tags.
- **File format version:** there's an optional top-level `format: 1` field; v1 rejects any other value. Once v2 exists, the CLI should use it to hand each file to the right package.
- The **history table** layout (`version BIGINT PK, name, checksum, applied_at`) is part of the v1 contract.
- **Up-only** migrations; there is no down migration.
- **Checksum mismatch fails by default.** It's opt-out via `WithIgnoreChecksums()` / `-ignore-checksums`, which logs a warning, doesn't re-run the file, and leaves the stored checksum alone. The user wants this so an edited old migration reaches new installs while existing installs keep what they ran.
- Every migration runs in a **transaction by default**; `transaction: false` opts out. Non-transactional failures say explicitly that the earlier statements were not rolled back.
- **Advisory lock:** `pg_advisory_lock(fnv64("polyschema:"+table))` on Postgres and `GET_LOCK(name, -1)` on MySQL. SQLite gets no lock (documented with a `ponytail:` comment).
- **Drivers are imported only by the CLI and tests.** The library takes a `*sql.DB`. The SQLite driver is `github.com/glebarez/go-sqlite` (pure Go, driver name `sqlite`); the user chose it. Postgres uses pgx stdlib (`pgx`), and MySQL uses go-sql-driver (`mysql`).
- **One parser:** `gopkg.in/yaml.v3` reads `.json` too. This was tested, and tab-indented JSON works. The decoder runs with `KnownFields(true)`, so typos and unknown operations are errors.
- **Engine gaps are reported, not emulated.** Each gap produces an `Issue` with severity `Warning` (still runs) or `Error` (refused). `Migrator` renders *every* file before touching the DB, so Error issues fail fast. There is deliberately no SQLite table rebuild; the `sql:` operation is the escape hatch.
- The CLI handles MySQL and MariaDB through the same driver.

## Implementation conventions and gotchas
- `Dialect` is a struct with a `family` field (`pg`/`my`/`lite`), not an interface. MariaDB and MySQL share `family == my`; compare `d == MySQL` only where MariaDB behaves differently (IF [NOT] EXISTS on indexes/columns, `DROP CHECK` vs `DROP CONSTRAINT`, literal defaults on text/json/binary).
- `typeNames` holds `[3]string{pg, mysql, sqlite}` indexed by `family`, and doubles as the set of valid types. Adding a type means adding an entry there, updating the README table and extending `TestTypes`.
- **Identifiers** are always quoted (`"x"`, or backticks on MySQL, with embedded quotes doubled). **DML values** are always bind parameters (`$n` on Postgres, `?` elsewhere). Only DDL defaults are inlined via `literal()`. On MySQL that also escapes backslashes, which assumes the default sql_mode.
- Map keys in `insert`/`update`/`where` are **sorted** so the SQL is deterministic. Each insert row becomes its own statement. Nested maps and lists are JSON-encoded, which suits json columns.
- `update`/`delete` require either `where` or `all: true`, never both.
- `alterColumn` restates the full column definition. That maps to MySQL `MODIFY COLUMN` and to a combined Postgres `ALTER COLUMN ... TYPE/SET|DROP NOT NULL/SET|DROP DEFAULT`, so both engines end in the same state.
- `createTable`:
  - Validation fills in defaults (string length 255, the nested `table` on indexes/FKs, `PrimaryKey` from column flags).
  - `autoIncrement` must be the sole PK column. SQLite needs it as an inline `INTEGER PRIMARY KEY AUTOINCREMENT`, with no separate table-level PK.
  - On MySQL, indexes are inlined into CREATE TABLE, because MySQL has no `CREATE INDEX IF NOT EXISTS`. On other engines they become separate CREATE INDEX statements that inherit the table's `ifNotExists`.
- `defaultExpr`: `CURRENT_TIMESTAMP`, `CURRENT_DATE` and `CURRENT_TIME` are emitted bare, and anything else is wrapped in parentheses.
- Every Render on MySQL/MariaDB warns at op 0 when a transactional file contains DDL (implicit commit). Unit tests use `transaction: false` to keep that warning out of the way.
- `Statement.Op` / `Issue.Op` are 1-based; 0 means the whole file, or the history-row insert.
- The type `CheckConstraint` has that name because `Check` is the function.
- `migrator.apply` runs everything on a single `*sql.Conn`, which the session-level advisory locks require. Unlock uses `context.WithoutCancel`.

## Testing conventions
- Generator tests use `expect(t, opYAML, map[*Dialect]want)`, which checks the exact SQL plus an issue substring. MariaDB falls back to the MySQL expectation and SQLite to the Postgres one; `bt()` converts `"` to backticks.
- Migrator tests use `newFixture`, which prefixes table names with a timestamp so shared servers don't collide, and use `fstest.MapFS` for migrations. SQLite uses a temp *file* (an in-memory DB would give each connection its own database).
- Every new operation or engine gap needs: validation cases in `TestValidate`, per-engine SQL in `generate_test.go`, a README row, and golden files regenerated with `-update` if the samples change.
- Target coverage is at least 90% for `v1`; it was 96.7% at the time of writing.
