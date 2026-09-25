package polyschema

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"time"
)

// Load reads every migration file at the top level of fsys, sorted by
// version. Files whose names don't look like migrations are ignored.
func Load(fsys fs.FS) ([]*File, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var files []*File
	for _, e := range entries {
		if e.IsDir() || !fileRe.MatchString(e.Name()) {
			continue
		}
		f, err := LoadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	slices.SortStableFunc(files, func(a, b *File) int { return cmp.Compare(a.Version, b.Version) })
	for i := 1; i < len(files); i++ {
		if files[i].Version == files[i-1].Version {
			return nil, fmt.Errorf("%s and %s have the same version %d", files[i-1].Name, files[i].Name, files[i].Version)
		}
	}
	return files, nil
}

// LoadFile reads and parses one migration file.
func LoadFile(fsys fs.FS, name string) (*File, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	return Parse(name, data)
}

// Migrator applies migrations to one database.
type Migrator struct {
	db              *sql.DB
	d               *Dialect
	table           string
	log             *slog.Logger
	ignoreChecksums bool
}

// versionCol is the history table's primary key column.
const versionCol = "version"

type Option func(*Migrator)

// WithTable sets the history table name (default "schema_migrations").
func WithTable(name string) Option { return func(m *Migrator) { m.table = name } }

// WithLogger sets where progress and warnings are logged (default: discarded).
func WithLogger(l *slog.Logger) Option { return func(m *Migrator) { m.log = l } }

// WithIgnoreChecksums logs a warning instead of failing when an applied
// migration file has been edited since. The edited file is not re-run.
func WithIgnoreChecksums() Option { return func(m *Migrator) { m.ignoreChecksums = true } }

// New returns a Migrator that applies migrations to db using dialect d.
func New(db *sql.DB, d *Dialect, opts ...Option) *Migrator {
	m := &Migrator{db: db, d: d, table: "schema_migrations", log: slog.New(slog.DiscardHandler)}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Up applies every pending migration in fsys, in version order.
// Use fs.Sub to point at a subdirectory of an embed.FS.
func (m *Migrator) Up(ctx context.Context, fsys fs.FS) error {
	files, err := Load(fsys)
	if err != nil {
		return err
	}
	return m.apply(ctx, files)
}

// ApplyFile applies a single migration file unless it's already applied.
func (m *Migrator) ApplyFile(ctx context.Context, fsys fs.FS, name string) error {
	f, err := LoadFile(fsys, name)
	if err != nil {
		return err
	}
	return m.apply(ctx, []*File{f})
}

func (m *Migrator) apply(ctx context.Context, files []*File) error {
	// Render everything before touching the database, so unsupported
	// operations fail fast.
	plans := make([][]Statement, len(files))
	var errs []error
	for i, f := range files {
		stmts, issues := Render(f, m.d)
		for _, is := range issues {
			if is.Severity == Error {
				errs = append(errs, errors.New(is.String()))
			} else {
				m.log.Warn(is.String())
			}
		}
		plans[i] = stmts
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	conn, err := m.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	unlock, err := m.lock(ctx, conn)
	if err != nil {
		return err
	}
	defer unlock()
	if err := m.ensureTable(ctx, conn); err != nil {
		return fmt.Errorf("creating %s: %w", m.table, err)
	}
	applied, err := m.applied(ctx, conn)
	if err != nil {
		return err
	}
	for i, f := range files {
		if sum, ok := applied[f.Version]; ok {
			if sum != f.Checksum {
				msg := fmt.Sprintf("%s was changed after it was applied (checksum mismatch)", f.Name)
				if !m.ignoreChecksums {
					return errors.New(msg + "; pass WithIgnoreChecksums / -ignore-checksums to allow this")
				}
				m.log.Warn(msg + "; not re-running it")
			}
			continue
		}
		m.log.Info("applying migration", "file", f.Name, "engine", m.d.name)
		if err := m.run(ctx, conn, f, plans[i]); err != nil {
			return err
		}
	}
	return nil
}

func (m *Migrator) run(ctx context.Context, conn *sql.Conn, f *File, stmts []Statement) error {
	q, ph := m.d.q, m.d.ph
	record := Statement{
		SQL: fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s, %s, %s, %s)", q(m.table),
			m.d.qList([]string{versionCol, "name", "checksum", "applied_at"}), ph(1), ph(2), ph(3), ph(4)),
		Args: []any{f.Version, f.Name, f.Checksum, time.Now().UTC()},
	}
	var ex interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	} = conn
	var tx *sql.Tx
	if f.UseTransaction() {
		var err error
		if tx, err = conn.BeginTx(ctx, nil); err != nil {
			return err
		}
		ex = tx
	}
	for _, s := range append(stmts, record) {
		if _, err := ex.ExecContext(ctx, s.SQL, s.Args...); err != nil {
			where := fmt.Sprintf("operation %d", s.Op)
			if s.Op == 0 {
				where = "recording history"
			}
			err = fmt.Errorf("%s: %s: %w\n  sql: %s", f.Name, where, err, s.SQL)
			if tx != nil {
				return errors.Join(err, tx.Rollback())
			}
			return fmt.Errorf("%w\n  (transaction: false, so earlier statements in this file were NOT rolled back)", err)
		}
	}
	if tx != nil {
		return tx.Commit()
	}
	return nil
}

//nolint:goconst // portable type names read best as literals
func (m *Migrator) ensureTable(ctx context.Context, conn *sql.Conn) error {
	no := false
	g := &gen{d: m.d}
	g.createTable(&CreateTable{Name: m.table, IfNotExists: true, PrimaryKey: []string{versionCol}, Columns: []Column{
		{Name: versionCol, Type: "bigint"},
		{Name: "name", Type: "string", Length: 255, Nullable: &no},
		{Name: "checksum", Type: "string", Length: 64, Nullable: &no},
		{Name: "applied_at", Type: "timestamp", Nullable: &no},
	}})
	_, err := conn.ExecContext(ctx, g.stmts[0].SQL)
	return err
}

func (m *Migrator) applied(ctx context.Context, conn *sql.Conn) (map[int64]string, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf("SELECT %s, %s FROM %s", m.d.q(versionCol), m.d.q("checksum"), m.d.q(m.table)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]string{}
	for rows.Next() {
		var v int64
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			return nil, err
		}
		out[v] = strings.TrimSpace(sum)
	}
	return out, rows.Err()
}

// lock takes a session-level advisory lock so concurrent migrators queue up.
// ponytail: SQLite gets no lock; don't run two migrators against one SQLite file at once.
func (m *Migrator) lock(ctx context.Context, conn *sql.Conn) (func(), error) {
	name := "polyschema:" + m.table
	bg := context.WithoutCancel(ctx)
	switch m.d.family {
	case pg:
		h := fnv.New64a()
		_, _ = h.Write([]byte(name)) // hash.Hash never returns an error
		key := int64(h.Sum64())      //nolint:gosec // wrapping is fine; pg lock keys are signed bigints
		if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
			return nil, fmt.Errorf("taking migration lock: %w", err)
		}
		return func() { m.unlock(bg, conn, "SELECT pg_advisory_unlock($1)", key) }, nil
	case my:
		var got sql.NullInt64
		if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, -1)", name).Scan(&got); err != nil || got.Int64 != 1 {
			return nil, fmt.Errorf("taking migration lock %q failed (%v)", name, err)
		}
		return func() { m.unlock(bg, conn, "SELECT RELEASE_LOCK(?)", name) }, nil
	}
	return func() {}, nil
}

// unlock releases the advisory lock. A failure is only logged: the lock is
// session-level, so it goes away when the connection closes anyway.
func (m *Migrator) unlock(ctx context.Context, conn *sql.Conn, query string, arg any) {
	if _, err := conn.ExecContext(ctx, query, arg); err != nil {
		m.log.Warn("releasing migration lock", "error", err)
	}
}
