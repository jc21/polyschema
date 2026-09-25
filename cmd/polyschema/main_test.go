package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const migrations = "../../v1/testdata/migrations"

func cli(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-bogus"}, "flag provided but not defined"},
		{nil, "exactly one of -dir or -file"},
		{[]string{"-dir", "a", "-file", "b"}, "exactly one of -dir or -file"},
		{[]string{"-dir", "a", "-engine", "oracle"}, "unknown -engine oracle"},
		{[]string{"-dir", migrations, "-dry-run"}, "-dry-run needs -engine"},
		{[]string{"-dir", migrations, "-engine", "sqlite"}, "needs -engine and -dsn"},
	} {
		t.Setenv("POLYSCHEMA_DSN", "")
		code, _, stderr := cli(tc.args...)
		if code != 2 || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: code %d, stderr %q, want %q", tc.args, code, stderr, tc.want)
		}
	}
}

func TestCheck(t *testing.T) {
	code, out, _ := cli("-check", "-dir", migrations)
	if code != 0 || !strings.Contains(out, "mysql warning: mysql commits DDL implicitly") {
		t.Errorf("check: code %d, out %q", code, out)
	}
	if code, _, _ := cli("-check", "-strict", "-dir", migrations); code != 1 {
		t.Errorf("-strict with warnings: code %d", code)
	}
	if code, out, _ := cli("-check", "-strict", "-engine", "postgres", "-dir", migrations); code != 0 || out != "" {
		t.Errorf("postgres check: code %d, out %q", code, out)
	}

	dir := t.TempDir()
	bad := filepath.Join(dir, "1_alter.yml")
	os.WriteFile(bad, []byte("operations: [{alterColumn: {table: t, name: c, type: text}}]"), 0o644)
	if code, out, _ := cli("-check", "-engine", "sqlite", "-file", bad); code != 1 || !strings.Contains(out, "sqlite error") {
		t.Errorf("sqlite error check: code %d, out %q", code, out)
	}
	if code, _, stderr := cli("-check", "-file", filepath.Join(dir, "2_missing.yml")); code != 1 || stderr == "" {
		t.Errorf("missing file: code %d", code)
	}
	if code, _, _ := cli("-check", "-dir", filepath.Join(dir, "nope")); code != 1 {
		t.Errorf("missing dir: code %d", code)
	}
}

func TestDryRun(t *testing.T) {
	code, out, _ := cli("-dry-run", "-engine", "postgres", "-dir", migrations)
	for _, want := range []string{"-- 1_create_roles_users.yml", `CREATE TABLE "users"`, "-- args: [1 admin]", "COMMENT ON TABLE"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
	if code != 0 {
		t.Errorf("code %d", code)
	}
	if code, _, stderr := cli("-dry-run", "-engine", "mysql", "-dir", migrations); code != 0 || !strings.Contains(stderr, "implicitly") {
		t.Errorf("mysql dry-run: code %d, stderr %q", code, stderr)
	}
}

func TestApplySQLite(t *testing.T) {
	db := filepath.Join(t.TempDir(), "app.db")
	if code, _, stderr := cli("-engine", "sqlite", "-dsn", db, "-file", filepath.Join(migrations, "1_create_roles_users.yml")); code != 0 {
		t.Fatalf("apply file: %d %s", code, stderr)
	}
	t.Setenv("POLYSCHEMA_DSN", db)
	code, _, stderr := cli("-engine", "sqlite", "-dir", migrations, "-ignore-checksums", "-table", "schema_migrations")
	if code != 0 || strings.Count(stderr, "applying migration") != 2 {
		t.Fatalf("apply dir: %d %s", code, stderr)
	}
	// 10_nickname adds a column that now exists; force a rerun via a fresh history table.
	if code, _, stderr := cli("-engine", "sqlite", "-dir", migrations, "-table", "other_history"); code != 1 || !strings.Contains(stderr, "already exists") {
		t.Errorf("rerun into existing schema: %d %s", code, stderr)
	}
	if code, _, _ := cli("-engine", "postgres", "-dsn", "::bad::", "-dir", migrations); code != 1 {
		t.Errorf("bad dsn: code %d", code)
	}
}
