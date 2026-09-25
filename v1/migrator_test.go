package polyschema

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	_ "github.com/glebarez/go-sqlite"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed testdata/migrations
var embedded embed.FS

type engine struct {
	d  *Dialect
	db *sql.DB
}

// engines returns SQLite (always) plus any server given by env var:
// POLYSCHEMA_PG_DSN, POLYSCHEMA_MYSQL_DSN, POLYSCHEMA_MARIA_DSN.
func engines(t *testing.T) []engine {
	out := []engine{{SQLite, openDB(t, "sqlite", filepath.Join(t.TempDir(), "test.db"))}}
	for env, d := range map[string]*Dialect{"POLYSCHEMA_PG_DSN": Postgres, "POLYSCHEMA_MYSQL_DSN": MySQL, "POLYSCHEMA_MARIA_DSN": MariaDB} {
		if dsn := os.Getenv(env); dsn != "" {
			driver := map[*Dialect]string{Postgres: "pgx", MySQL: "mysql", MariaDB: "mysql"}[d]
			out = append(out, engine{d, openDB(t, driver, dsn)})
		}
	}
	return out
}

func openDB(t *testing.T, driver, dsn string) *sql.DB {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// fixture is a migration set whose table names carry a prefix, so runs on a
// shared server don't collide.
type fixture struct {
	p   string // prefix
	fs  fstest.MapFS
	e   engine
	log *bytes.Buffer
}

func newFixture(t *testing.T, e engine) *fixture {
	p := fmt.Sprintf("t%d_", time.Now().UnixNano())
	fx := &fixture{p: p, e: e, log: &bytes.Buffer{}, fs: fstest.MapFS{
		"1_schema.yml": {Data: fmt.Appendf(nil, `operations:
  - createTable:
      name: %[1]sroles
      columns:
        - {name: id, type: integer, primaryKey: true}
        - {name: name, type: string, length: 50, nullable: false}
  - createTable:
      name: %[1]susers
      columns:
        - {name: id, type: bigint, primaryKey: true, autoIncrement: true}
        - {name: email, type: string, length: 100, nullable: false, unique: true}
        - {name: role_id, type: integer}
        - {name: active, type: boolean, nullable: false, default: true}
        - {name: created_at, type: timestamp, nullable: false, defaultExpr: CURRENT_TIMESTAMP}
      indexes: [{name: %[1]sidx_role, columns: [role_id]}]
      foreignKeys: [{name: %[1]sfk_role, columns: [role_id], refTable: %[1]sroles, refColumns: [id]}]
`, p)},
		"2_seed.json": {Data: fmt.Appendf(nil, `{"operations": [
	{"insert": {"table": "%[1]sroles", "rows": [{"id": 1, "name": "admin"}, {"id": 2, "name": "user"}]}},
	{"insert": {"table": "%[1]susers", "rows": [{"email": "a@x", "role_id": 1}, {"email": "b@x", "role_id": 2}]}}
]}`, p)},
		"10_more.yml": {Data: fmt.Appendf(nil, `operations:
  - addColumn: {table: %[1]susers, column: {name: nick, type: string, length: 20}}
  - update: {table: %[1]susers, set: {nick: boss}, where: {role_id: 1}}
  - delete: {table: %[1]susers, where: {role_id: 2}}
`, p)},
	}}
	t.Cleanup(func() {
		for _, tbl := range []string{"users", "roles", "x", "y", "z", "migrations"} {
			e.db.Exec("DROP TABLE IF EXISTS " + e.d.q(p+tbl))
		}
	})
	return fx
}

func (fx *fixture) migrator(opts ...Option) *Migrator {
	opts = append([]Option{WithTable(fx.p + "migrations"), WithLogger(slog.New(slog.NewTextHandler(fx.log, nil)))}, opts...)
	return New(fx.e.db, fx.e.d, opts...)
}

func (fx *fixture) count(t *testing.T, table, where string) int {
	t.Helper()
	var n int
	if err := fx.e.db.QueryRow("SELECT COUNT(*) FROM " + fx.e.d.q(fx.p+table) + where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (fx *fixture) exists(table string) bool {
	_, err := fx.e.db.Exec("SELECT 1 FROM " + fx.e.d.q(fx.p+table) + " WHERE 1 = 0")
	return err == nil
}

func TestMigrator(t *testing.T) {
	ctx := context.Background()
	for _, e := range engines(t) {
		t.Run(e.d.name, func(t *testing.T) {
			fx := newFixture(t, e)
			m := fx.migrator()

			if err := m.Up(ctx, fx.fs); err != nil {
				t.Fatal(err)
			}
			if n := fx.count(t, "migrations", ""); n != 3 {
				t.Fatalf("history rows = %d", n)
			}
			if n := fx.count(t, "users", " WHERE nick = 'boss'"); n != 1 {
				t.Errorf("users with nick = %d", n)
			}
			if n := fx.count(t, "users", ""); n != 1 {
				t.Errorf("users = %d", n)
			}

			// Re-running is a no-op.
			if err := m.Up(ctx, fx.fs); err != nil {
				t.Fatal(err)
			}
			if n := fx.count(t, "migrations", ""); n != 3 {
				t.Fatalf("history rows after rerun = %d", n)
			}

			// Editing an applied file fails by default, warns when opted out.
			edited := fstest.MapFS{"2_seed.json": {Data: append(fx.fs["2_seed.json"].Data, '\n')}}
			if err := m.Up(ctx, edited); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
				t.Errorf("edited file: %v", err)
			}
			if err := fx.migrator(WithIgnoreChecksums()).Up(ctx, edited); err != nil {
				t.Errorf("edited file with WithIgnoreChecksums: %v", err)
			}
			if !strings.Contains(fx.log.String(), "not re-running it") {
				t.Errorf("expected checksum warning in log: %s", fx.log)
			}

			// A failure inside a transaction rolls the whole file back.
			fx.fs["20_bad.yml"] = &fstest.MapFile{Data: fmt.Appendf(nil, `operations:
  - createTable: {name: %[1]sx, columns: [{name: id, type: integer}]}
  - insert: {table: %[1]snope, rows: [{a: 1}]}
`, fx.p)}
			err := m.Up(ctx, fx.fs)
			if err == nil || !strings.Contains(err.Error(), "20_bad.yml: operation 2") {
				t.Fatalf("bad migration: %v", err)
			}
			if e.d.family != my && fx.exists("x") { // MySQL commits DDL implicitly
				t.Error("table x survived the rollback")
			}
			if n := fx.count(t, "migrations", ""); n != 3 {
				t.Errorf("history rows after failure = %d", n)
			}
			delete(fx.fs, "20_bad.yml")

			// Without a transaction, earlier statements stay applied.
			fx.fs["21_notx.yml"] = &fstest.MapFile{Data: fmt.Appendf(nil, `transaction: false
operations:
  - createTable: {name: %[1]sy, columns: [{name: id, type: integer}]}
  - insert: {table: %[1]snope, rows: [{a: 1}]}
`, fx.p)}
			if err := m.Up(ctx, fx.fs); err == nil || !strings.Contains(err.Error(), "NOT rolled back") {
				t.Fatalf("non-transactional failure: %v", err)
			}
			if !fx.exists("y") {
				t.Error("table y should exist after a non-transactional failure")
			}
			delete(fx.fs, "21_notx.yml")

			// ApplyFile runs one file, once.
			fx.fs["30_one.yml"] = &fstest.MapFile{Data: fmt.Appendf(nil,
				"operations: [{createTable: {name: %sz, columns: [{name: id, type: integer}]}}]", fx.p)}
			for range 2 {
				if err := m.ApplyFile(ctx, fx.fs, "30_one.yml"); err != nil {
					t.Fatal(err)
				}
			}
			if n := fx.count(t, "migrations", ""); n != 4 {
				t.Errorf("history rows after ApplyFile = %d", n)
			}
		})
	}
}

func TestMigratorRefusesUnsupported(t *testing.T) {
	e := engines(t)[0]
	fx := newFixture(t, e)
	fsys := fstest.MapFS{"1_a.yml": {Data: []byte("operations: [{alterColumn: {table: t, name: c, type: text}}, {sql: {postgres: x}}]")}}
	err := fx.migrator().Up(context.Background(), fsys)
	if err == nil || !strings.Contains(err.Error(), "table rebuild") || !strings.Contains(err.Error(), "no entry for sqlite") {
		t.Errorf("got %v", err)
	}
	if fx.exists("migrations") {
		t.Error("nothing should touch the database when rendering fails")
	}
}

func TestMigratorErrors(t *testing.T) {
	ctx := context.Background()
	e := engines(t)[0]
	fx := newFixture(t, e)
	m := fx.migrator()
	if err := m.Up(ctx, fstest.MapFS{"1_a.yml": {Data: []byte("bad")}}); err == nil {
		t.Error("Up should surface load errors")
	}
	if err := m.ApplyFile(ctx, fstest.MapFS{}, "1_a.yml"); err == nil {
		t.Error("ApplyFile should surface load errors")
	}
	closed := openDB(t, "sqlite", filepath.Join(t.TempDir(), "c.db"))
	closed.Close()
	if err := New(closed, SQLite).Up(ctx, fx.fs); err == nil {
		t.Error("closed db should fail")
	}
	// A history table that exists with the wrong shape can't be read.
	if _, err := e.db.Exec("CREATE TABLE " + e.d.q(fx.p+"migrations") + " (other INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(ctx, fx.fs); err == nil {
		t.Error("broken history table should fail")
	}
}

func TestEmbedFS(t *testing.T) {
	sub, err := fs.Sub(embedded, "testdata/migrations")
	if err != nil {
		t.Fatal(err)
	}
	e := engines(t)[0]
	if err := New(e.db, e.d).Up(context.Background(), sub); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM "users" WHERE "handle" = 'boss'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("n = %d, err = %v", n, err)
	}
}

// TestLock runs two migrators at once; the advisory lock must serialise them.
func TestLock(t *testing.T) {
	for _, e := range engines(t) {
		if e.d == SQLite {
			continue // no advisory lock on SQLite
		}
		t.Run(e.d.name, func(t *testing.T) {
			fx := newFixture(t, e)
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i := range errs {
				wg.Go(func() {
					errs[i] = fx.migrator(WithLogger(slog.New(slog.DiscardHandler))).Up(context.Background(), fx.fs)
				})
			}
			wg.Wait()
			for _, err := range errs {
				if err != nil {
					t.Error(err)
				}
			}
			if n := fx.count(t, "migrations", ""); n != 3 {
				t.Errorf("history rows = %d", n)
			}
		})
	}
}
