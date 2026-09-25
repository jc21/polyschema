package polyschema

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

const (
	pg = iota
	my
	lite
)

// Dialect is a target database engine. Use the package variables.
type Dialect struct {
	name   string
	family int // index into typeNames entries
}

var (
	Postgres = &Dialect{"postgres", pg}
	MySQL    = &Dialect{"mysql", my}
	MariaDB  = &Dialect{"mariadb", my}
	SQLite   = &Dialect{"sqlite", lite}

	// All lists every supported dialect.
	All = []*Dialect{Postgres, MySQL, MariaDB, SQLite}
)

func (d *Dialect) String() string { return d.name }

// DialectByName returns the dialect called name, or nil.
func DialectByName(name string) *Dialect {
	for _, d := range All {
		if d.name == strings.ToLower(name) {
			return d
		}
	}
	return nil
}

// q quotes an identifier.
func (d *Dialect) q(id string) string {
	if d.family == my {
		return "`" + strings.ReplaceAll(id, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(id, `"`, `""`) + `"`
}

func (d *Dialect) qList(ids []string) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = d.q(id)
	}
	return strings.Join(out, ", ")
}

// ph is the placeholder for the n-th (1-based) statement argument.
func (d *Dialect) ph(n int) string {
	if d.family == pg {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// literal renders a default value inline; DDL can't take bind parameters.
func (d *Dialect) literal(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case bool:
		if d.family == pg {
			return strings.ToUpper(fmt.Sprint(x))
		}
		if x {
			return "1"
		}
		return "0"
	case string:
		s := strings.ReplaceAll(x, "'", "''")
		if d.family == my {
			// ponytail: assumes the default sql_mode; wrong under NO_BACKSLASH_ESCAPES.
			s = strings.ReplaceAll(s, `\`, `\\`)
		}
		return "'" + s + "'"
	default: // numbers; Column.validate rejects anything else
		return fmt.Sprint(x)
	}
}

// typeNames maps portable types to {postgres, mysql/mariadb, sqlite}.
// string takes %d = length; decimal takes %d,%d = precision, scale.
//
//nolint:goconst // a lookup table reads best as literals
var typeNames = map[string][3]string{
	"smallint":    {"SMALLINT", "SMALLINT", "INTEGER"},
	"integer":     {"INTEGER", "INTEGER", "INTEGER"},
	"bigint":      {"BIGINT", "BIGINT", "INTEGER"},
	"string":      {"VARCHAR(%d)", "VARCHAR(%d)", "TEXT"},
	"text":        {"TEXT", "TEXT", "TEXT"},
	"boolean":     {"BOOLEAN", "TINYINT(1)", "INTEGER"},
	"decimal":     {"NUMERIC(%d,%d)", "DECIMAL(%d,%d)", "NUMERIC"},
	"float":       {"REAL", "FLOAT", "REAL"},
	"double":      {"DOUBLE PRECISION", "DOUBLE", "REAL"},
	"date":        {"DATE", "DATE", "TEXT"},
	"time":        {"TIME", "TIME", "TEXT"},
	"timestamp":   {"TIMESTAMP", "DATETIME", "TEXT"},
	"timestamptz": {"TIMESTAMPTZ", "DATETIME", "TEXT"},
	"json":        {"JSONB", "JSON", "TEXT"},
	"uuid":        {"UUID", "CHAR(36)", "TEXT"},
	"binary":      {"BYTEA", "BLOB", "BLOB"},
}

func typeList() []string { return slices.Sorted(maps.Keys(typeNames)) }

func (d *Dialect) columnType(c *Column) string {
	t := typeNames[c.Type][d.family]
	if !strings.Contains(t, "%") {
		return t
	}
	if c.Type == "string" {
		return fmt.Sprintf(t, c.Length)
	}
	return fmt.Sprintf(t, c.Precision, c.Scale)
}
