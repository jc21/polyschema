// Command polyschema applies, checks or prints portable migration files.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	polyschema "github.com/jc21/polyschema/v1"

	_ "github.com/glebarez/go-sqlite"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// mysqlDriver serves both MySQL and MariaDB.
const mysqlDriver = "mysql"

var drivers = map[string]string{"postgres": "pgx", "mysql": mysqlDriver, "mariadb": mysqlDriver, "sqlite": "sqlite"}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("polyschema", flag.ContinueOnError)
	fl.SetOutput(stderr)
	engine := fl.String("engine", "", "postgres, mysql, mariadb or sqlite")
	dsn := fl.String("dsn", os.Getenv("POLYSCHEMA_DSN"), "connection string (default $POLYSCHEMA_DSN)")
	dir := fl.String("dir", "", "folder of migration files")
	file := fl.String("file", "", "a single migration file")
	check := fl.Bool("check", false, "report engine support issues; all engines unless -engine is set")
	strict := fl.Bool("strict", false, "with -check, fail on warnings too")
	dryRun := fl.Bool("dry-run", false, "print the SQL for -engine instead of running it")
	ignore := fl.Bool("ignore-checksums", false, "warn instead of failing when an applied file was edited")
	table := fl.String("table", "schema_migrations", "migration history table")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	usage := func(msg string) int {
		fmt.Fprintln(stderr, "polyschema:", msg)
		fl.Usage()
		return 2
	}
	if (*dir == "") == (*file == "") {
		return usage("give exactly one of -dir or -file")
	}
	var d *polyschema.Dialect
	if *engine != "" {
		if d = polyschema.DialectByName(*engine); d == nil {
			return usage("unknown -engine " + *engine)
		}
	}

	if *check || *dryRun {
		files, err := load(*dir, *file)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if *check {
			dialects := polyschema.All
			if d != nil {
				dialects = []*polyschema.Dialect{d}
			}
			return report(polyschema.Check(files, dialects...), *strict, stdout)
		}
		if d == nil {
			return usage("-dry-run needs -engine")
		}
		code := 0
		for _, f := range files {
			stmts, issues := polyschema.Render(f, d)
			fmt.Fprintf(stdout, "-- %s\n", f.Name)
			for _, s := range stmts {
				fmt.Fprintf(stdout, "%s;\n", s.SQL)
				if len(s.Args) > 0 {
					fmt.Fprintf(stdout, "-- args: %v\n", s.Args)
				}
			}
			code = max(code, report(issues, false, stderr))
		}
		return code
	}

	if d == nil || *dsn == "" {
		return usage("applying migrations needs -engine and -dsn (or $POLYSCHEMA_DSN)")
	}
	db, err := sql.Open(drivers[d.String()], *dsn)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = db.Close() }()
	opts := []polyschema.Option{polyschema.WithTable(*table), polyschema.WithLogger(slog.New(slog.NewTextHandler(stderr, nil)))}
	if *ignore {
		opts = append(opts, polyschema.WithIgnoreChecksums())
	}
	m := polyschema.New(db, d, opts...)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *dir != "" {
		err = m.Up(ctx, os.DirFS(*dir))
	} else {
		err = m.ApplyFile(ctx, os.DirFS(filepath.Dir(*file)), filepath.Base(*file))
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func load(dir, file string) ([]*polyschema.File, error) {
	if dir != "" {
		return polyschema.Load(os.DirFS(dir))
	}
	f, err := polyschema.LoadFile(os.DirFS(filepath.Dir(file)), filepath.Base(file))
	if err != nil {
		return nil, err
	}
	return []*polyschema.File{f}, nil
}

// report prints issues and returns 1 if any is an error (or any at all, when strict).
func report(issues []polyschema.Issue, strict bool, w io.Writer) int {
	code := 0
	for _, is := range issues {
		fmt.Fprintln(w, is)
		if strict || is.Severity == polyschema.Error {
			code = 1
		}
	}
	return code
}
