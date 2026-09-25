package polyschema

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// ops parses a migration whose operations list is body.
func ops(t *testing.T, body string) *File {
	t.Helper()
	f, err := Parse("1_test.yml", []byte("operations:\n"+body))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParseYAMLAndJSON(t *testing.T) {
	y, err := Parse("dir/0042_a.yaml", []byte("transaction: false\noperations:\n  - dropTable: {name: a}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if y.Name != "0042_a.yaml" || y.Version != 42 || y.Format != 1 || y.UseTransaction() || len(y.Checksum) != 64 {
		t.Errorf("unexpected file: %+v", y)
	}
	j, err := Parse("7-b.json", []byte("{\n\t\"format\": 1,\n\t\"operations\": [{\"dropTable\": {\"name\": \"a\"}}]\n}"))
	if err != nil {
		t.Fatal(err)
	}
	if j.Version != 7 || !j.UseTransaction() || j.Operations[0].DropTable.Name != "a" {
		t.Errorf("unexpected file: %+v", j)
	}
	if _, err := Parse("3.yml", []byte("operations: [{dropTable: {name: a}}]")); err != nil {
		t.Errorf("bare version filename: %v", err)
	}
}

func TestParseErrors(t *testing.T) {
	for _, tc := range []struct{ name, data, want string }{
		{"x.yml", "", "filename must look like"},
		{"1_a.txt", "", "filename must look like"},
		{"99999999999999999999_a.yml", "", "bad version"},
		{"1_a.yml", "", "no operations"},
		{"1_a.yml", "operations: []", "no operations"},
		{"1_a.yml", "format: 2\noperations: [{dropTable: {name: a}}]", "unsupported format 2"},
		{"1_a.yml", "format: 0\noperations: [{dropTable: {name: a}}]", "unsupported format 0"},
		{"1_a.yml", "operations: [{dropTable: {name: a, bogus: 1}}]", "field bogus not found"},
		{"1_a.yml", "operations: [{nope: {}}]", "field nope not found"},
		{"1_a.yml", "operations: [", "did not find expected"},
	} {
		_, err := Parse(tc.name, []byte(tc.data))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %q: got %v, want %q", tc.name, tc.data, err, tc.want)
		}
	}
}

func TestValidate(t *testing.T) {
	col := "columns: [{name: id, type: integer}]"
	for _, tc := range []struct{ op, want string }{
		{"{}", "needs exactly one of createTable"},
		{"{dropTable: {name: a}, renameTable: {from: a, to: b}}", "(got 2)"},
		{"dropTable: {}", "dropTable.name is required"},
		{"renameTable: {from: a}", "renameTable.to is required"},
		{"addColumn: {column: {name: a, type: text}}", "addColumn.table is required"},
		{"addColumn: {table: t, column: {name: a, type: integer, primaryKey: true}}", "can't be primaryKey"},
		{"addColumn: {table: t, column: {name: a, type: nope}}", `unknown type "nope"`},
		{"dropColumn: {table: t}", "dropColumn.name is required"},
		{"renameColumn: {table: t, from: a}", "renameColumn.to is required"},
		{"alterColumn: {name: a, type: text}", "alterColumn.table is required"},
		{"alterColumn: {table: t, name: a, type: text, unique: true}", "can't be changed here"},
		{"alterColumn: {table: t, name: a}", "unknown type"},
		{"createIndex: {table: t, name: i}", "index i: columns is required"},
		{"createIndex: {table: t, columns: [a]}", "index.name is required"},
		{"dropIndex: {name: i}", "dropIndex.table is required"},
		{"addUnique: {table: t, columns: [a]}", "unique.name is required"},
		{"addUnique: {table: t, name: u}", "unique u: columns is required"},
		{"addForeignKey: {table: t, name: f, columns: [a], refColumns: [id]}", "foreignKey.refTable is required"},
		{"addForeignKey: {table: t, name: f, refTable: r, refColumns: [id]}", "columns is required"},
		{"addForeignKey: {table: t, name: f, columns: [a, b], refTable: r, refColumns: [id]}", "same length"},
		{"addForeignKey: {table: t, name: f, columns: [a], refTable: r, refColumns: [id], onDelete: explode}", `action "explode"`},
		{"addCheck: {table: t, name: c}", "check.expr is required"},
		{"dropUnique: {table: t}", "name is required"},
		{"dropForeignKey: {name: f}", "table is required"},
		{"dropCheck: {table: t}", "name is required"},
		{"insert: {rows: [{a: 1}]}", "insert.table is required"},
		{"insert: {table: t}", "insert.rows is required"},
		{"insert: {table: t, rows: [{}]}", "insert.rows[0] is empty"},
		{"update: {set: {a: 1}, all: true}", "update.table is required"},
		{"update: {table: t, all: true}", "update.set is required"},
		{"update: {table: t, set: {a: 1}}", "needs where, or all: true"},
		{"update: {table: t, set: {a: 1}, where: {b: 1}, all: true}", "not both"},
		{"delete: {where: {a: 1}}", "delete.table is required"},
		{"delete: {table: t}", "needs where"},
		{"sql: {}", "sql needs at least one"},
		{"sql: {oracle: x}", `unknown engine "oracle"`},
		{"createTable: {" + col + "}", "createTable.name is required"},
		{"createTable: {name: t}", "createTable.columns is required"},
		{"createTable: {name: t, columns: [{name: a, type: text}, {name: a, type: text}]}", `duplicate column "a"`},
		{"createTable: {name: t, columns: [{type: text}]}", "column.name is required"},
		{"createTable: {name: t, primaryKey: [id], columns: [{name: id, type: integer, primaryKey: true}]}", "not both"},
		{"createTable: {name: t, primaryKey: [nope], " + col + "}", `primaryKey: unknown column "nope"`},
		{"createTable: {name: t, columns: [{name: id, type: integer, autoIncrement: true}]}", "only primary key column"},
		{"createTable: {name: t, primaryKey: [a, b], columns: [{name: a, type: integer, autoIncrement: true}, {name: b, type: integer}]}", "only primary key column"},
		{"createTable: {name: t, columns: [{name: id, type: text, autoIncrement: true, primaryKey: true}]}", "needs an integer type"},
		{"createTable: {name: t, columns: [{name: id, type: integer, autoIncrement: true, primaryKey: true, default: 1}]}", "can't have a default"},
		{"createTable: {name: t, columns: [{name: d, type: decimal}]}", "decimal needs precision"},
		{"createTable: {name: t, columns: [{name: d, type: decimal, precision: 2, scale: 3}]}", "decimal needs precision"},
		{"createTable: {name: t, columns: [{name: s, type: text, length: -1}]}", "negative length"},
		{"createTable: {name: t, columns: [{name: s, type: text, default: a, defaultExpr: b}]}", "not both"},
		{"createTable: {name: t, columns: [{name: s, type: text, default: [1]}]}", "default must be"},
		{"createTable: {name: t, " + col + ", indexes: [{table: x, name: i, columns: [id]}]}", `belongs to table "x"`},
		{"createTable: {name: t, " + col + ", indexes: [{name: i}]}", "columns is required"},
		{"createTable: {name: t, " + col + ", indexes: [{name: i, columns: [zz]}]}", `index i: unknown column "zz"`},
		{"createTable: {name: t, " + col + ", uniques: [{table: x, name: u, columns: [id]}]}", `belongs to table "x"`},
		{"createTable: {name: t, " + col + ", uniques: [{name: u}]}", "columns is required"},
		{"createTable: {name: t, " + col + ", uniques: [{name: u, columns: [zz]}]}", "unknown column"},
		{"createTable: {name: t, " + col + ", foreignKeys: [{table: x, name: f}]}", `belongs to table "x"`},
		{"createTable: {name: t, " + col + ", foreignKeys: [{name: f, columns: [id]}]}", "refTable is required"},
		{"createTable: {name: t, " + col + ", foreignKeys: [{name: f, columns: [zz], refTable: r, refColumns: [id]}]}", "unknown column"},
		{"createTable: {name: t, " + col + ", checks: [{table: x, name: c, expr: y}]}", `belongs to table "x"`},
		{"createTable: {name: t, " + col + ", checks: [{name: c}]}", "check.expr is required"},
	} {
		_, err := Parse("1_a.yml", []byte("operations:\n  - "+tc.op))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s:\n  got  %v\n  want %q", tc.op, err, tc.want)
		}
	}
}

func TestValidateFillsDefaults(t *testing.T) {
	f := ops(t, `  - createTable:
      name: t
      columns: [{name: id, type: integer, primaryKey: true}, {name: s, type: string}]
      indexes: [{name: i, columns: [s]}]
`)
	ct := f.Operations[0].CreateTable
	if ct.Columns[1].Length != 255 || ct.Indexes[0].Table != "t" || ct.PrimaryKey[0] != "id" {
		t.Errorf("defaults not filled: %+v", ct)
	}
}

func TestLoad(t *testing.T) {
	op := &fstest.MapFile{Data: []byte("operations: [{dropTable: {name: a}}]")}
	files, err := Load(fstest.MapFS{
		"10_c.yml":      op,
		"2_b.json":      {Data: []byte(`{"operations": [{"dropTable": {"name": "a"}}]}`)},
		"1_a.yaml":      op,
		"README.md":     {Data: []byte("ignored")},
		"3_dir/4_x.yml": op, // subdirectories are ignored
	})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(files))
	for _, f := range files {
		got = append(got, f.Name)
	}
	if strings.Join(got, ",") != "1_a.yaml,2_b.json,10_c.yml" {
		t.Errorf("order = %v", got)
	}

	if _, err := Load(fstest.MapFS{"1_a.yml": op, "01_b.yml": op}); err == nil || !strings.Contains(err.Error(), "same version 1") {
		t.Errorf("duplicate versions: %v", err)
	}
	if _, err := Load(fstest.MapFS{"1_a.yml": {Data: []byte("x: 1")}}); err == nil {
		t.Error("bad file should fail Load")
	}
	if _, err := Load(os.DirFS(t.TempDir() + "/missing")); err == nil {
		t.Error("missing dir should fail Load")
	}
	if _, err := LoadFile(fstest.MapFS{}, "1_a.yml"); err == nil {
		t.Error("missing file should fail LoadFile")
	}
}
