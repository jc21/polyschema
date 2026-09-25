package polyschema

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"reflect"
	"slices"
	"strings"
)

type Severity int

const (
	// Warning: the operation runs, with a degraded result.
	Warning Severity = iota
	// Error: the operation can't run on this engine.
	Error
)

func (s Severity) String() string {
	if s == Error {
		return "error"
	}
	return "warning"
}

// Issue is an engine-support problem found while rendering a file.
type Issue struct {
	File     string
	Op       int // 1-based operation index; 0 = the whole file
	Engine   string
	Severity Severity
	Message  string
}

func (i Issue) String() string {
	return fmt.Sprintf("%s:%d %s %s: %s", i.File, i.Op, i.Engine, i.Severity, i.Message)
}

// Statement is one SQL statement with its bind arguments.
type Statement struct {
	Op   int // 1-based operation index it came from
	SQL  string
	Args []any
}

// Render turns a file into SQL for d. Issues with Severity Error mean the
// file must not be run on d.
func Render(f *File, d *Dialect) ([]Statement, []Issue) {
	g := &gen{d: d, file: f.Name}
	ddl := false
	for i := range f.Operations {
		g.op = i + 1
		o := &f.Operations[i]
		g.operation(o)
		ddl = ddl || (o.Insert == nil && o.Update == nil && o.Delete == nil && o.SQL == nil)
	}
	if d.family == my && ddl && f.UseTransaction() {
		g.op = 0
		g.warn("%s commits DDL implicitly, so a failed migration can't be fully rolled back", d)
	}
	return g.stmts, g.issues
}

// Check renders every file for every dialect and returns all issues.
func Check(files []*File, dialects ...*Dialect) []Issue {
	var out []Issue
	for _, f := range files {
		for _, d := range dialects {
			_, issues := Render(f, d)
			out = append(out, issues...)
		}
	}
	return out
}

// CheckFS loads the migrations in fsys and checks them against dialects.
func CheckFS(fsys fs.FS, dialects ...*Dialect) ([]Issue, error) {
	files, err := Load(fsys)
	if err != nil {
		return nil, err
	}
	return Check(files, dialects...), nil
}

const kwIfNotExists = "IF NOT EXISTS "

type gen struct {
	d      *Dialect
	file   string
	op     int
	stmts  []Statement
	issues []Issue
}

func (g *gen) emit(sql string, args ...any) {
	g.stmts = append(g.stmts, Statement{Op: g.op, SQL: sql, Args: args})
}

func (g *gen) issue(s Severity, format string, args ...any) {
	g.issues = append(g.issues, Issue{g.file, g.op, g.d.name, s, fmt.Sprintf(format, args...)})
}

func (g *gen) warn(format string, args ...any) { g.issue(Warning, format, args...) }
func (g *gen) fail(format string, args ...any) { g.issue(Error, format, args...) }

func (g *gen) operation(o *Operation) {
	q := g.d.q
	switch {
	case o.CreateTable != nil:
		g.createTable(o.CreateTable)
	case o.DropTable != nil:
		g.dropTable(o.DropTable)
	case o.RenameTable != nil:
		g.emit(fmt.Sprintf("ALTER TABLE %s RENAME TO %s", q(o.RenameTable.From), q(o.RenameTable.To)))
	case o.AddColumn != nil:
		g.addColumn(o.AddColumn)
	case o.DropColumn != nil:
		c := o.DropColumn
		g.emit(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s%s", q(c.Table), g.ifExists(c.IfExists, "column"), q(c.Name)))
	case o.RenameColumn != nil:
		c := o.RenameColumn
		g.emit(fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", q(c.Table), q(c.From), q(c.To)))
	case o.AlterColumn != nil:
		g.alterColumn(o.AlterColumn)
	case o.CreateIndex != nil:
		g.createIndex(o.CreateIndex)
	case o.DropIndex != nil:
		g.dropIndex(o.DropIndex)
	case o.AddUnique != nil:
		g.addUnique(o.AddUnique)
	case o.DropUnique != nil:
		g.dropUnique(o.DropUnique)
	case o.AddForeignKey != nil:
		g.addForeignKey(o.AddForeignKey)
	case o.DropForeignKey != nil:
		g.dropForeignKey(o.DropForeignKey)
	case o.AddCheck != nil:
		g.addCheck(o.AddCheck)
	case o.DropCheck != nil:
		g.dropCheck(o.DropCheck)
	case o.Insert != nil:
		g.insert(o.Insert)
	case o.Update != nil:
		g.update(o.Update)
	case o.Delete != nil:
		var args []any
		g.emit("DELETE FROM "+q(o.Delete.Table)+g.where(o.Delete.Where, &args), args...)
	default:
		g.rawSQL(o.SQL)
	}
}

func (g *gen) dropTable(t *DropTable) {
	d := g.d
	s := "DROP TABLE " + g.ifExists(t.IfExists, "table") + d.q(t.Name)
	if t.Cascade {
		if d.family == pg {
			s += " CASCADE"
		} else {
			g.warn("%s ignores cascade on drop table; drop dependent objects first", d)
		}
	}
	g.emit(s)
}

func (g *gen) dropIndex(ix *DropIndex) {
	s := "DROP INDEX " + g.ifExists(ix.IfExists, "index") + g.d.q(ix.Name)
	if g.d.family == my {
		s += " ON " + g.d.q(ix.Table)
	}
	g.emit(s)
}

func (g *gen) addUnique(u *Unique) {
	d, q := g.d, g.d.q
	if d.family == lite {
		g.warn("SQLite can't add constraints to an existing table; creating unique index %s instead", u.Name)
		g.emit(fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", q(u.Name), q(u.Table), d.qList(u.Columns)))
		return
	}
	g.emit(fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s)", q(u.Table), q(u.Name), d.qList(u.Columns)))
}

func (g *gen) dropUnique(u *DropConstraint) {
	q := g.d.q
	switch g.d.family {
	case lite:
		g.warn("SQLite can't drop constraints; dropping index %s instead (works for uniques added by addUnique)", u.Name)
		g.emit("DROP INDEX " + q(u.Name))
	case my:
		g.emit(fmt.Sprintf("ALTER TABLE %s DROP INDEX %s", q(u.Table), q(u.Name)))
	default:
		g.dropConstraint(u)
	}
}

func (g *gen) addForeignKey(fk *ForeignKey) {
	if g.rebuildNeeded("add a foreign key to") {
		return
	}
	g.emit(fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s", g.d.q(fk.Table), g.d.q(fk.Name), g.fkClause(fk)))
}

func (g *gen) dropForeignKey(fk *DropConstraint) {
	if g.rebuildNeeded("drop a foreign key from") {
		return
	}
	if g.d.family == my {
		g.emit(fmt.Sprintf("ALTER TABLE %s DROP FOREIGN KEY %s", g.d.q(fk.Table), g.d.q(fk.Name)))
		return
	}
	g.dropConstraint(fk)
}

func (g *gen) addCheck(ck *CheckConstraint) {
	if g.rebuildNeeded("add a check constraint to") {
		return
	}
	g.emit(fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", g.d.q(ck.Table), g.d.q(ck.Name), ck.Expr))
}

func (g *gen) dropCheck(ck *DropConstraint) {
	if g.rebuildNeeded("drop a check constraint from") {
		return
	}
	if g.d == MySQL {
		g.emit(fmt.Sprintf("ALTER TABLE %s DROP CHECK %s", g.d.q(ck.Table), g.d.q(ck.Name)))
		return
	}
	g.dropConstraint(ck)
}

func (g *gen) insert(ins *Insert) {
	d, q := g.d, g.d.q
	for _, row := range ins.Rows {
		var cols, phs []string
		var args []any
		for _, k := range slices.Sorted(maps.Keys(row)) {
			cols = append(cols, q(k))
			args = append(args, g.arg(row[k]))
			phs = append(phs, d.ph(len(args)))
		}
		g.emit(fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", q(ins.Table), strings.Join(cols, ", "), strings.Join(phs, ", ")), args...)
	}
}

func (g *gen) update(u *Update) {
	q := g.d.q
	sets := make([]string, 0, len(u.Set))
	args := make([]any, 0, len(u.Set)+len(u.Where))
	for _, k := range slices.Sorted(maps.Keys(u.Set)) {
		args = append(args, g.arg(u.Set[k]))
		sets = append(sets, q(k)+" = "+g.d.ph(len(args)))
	}
	g.emit("UPDATE "+q(u.Table)+" SET "+strings.Join(sets, ", ")+g.where(u.Where, &args), args...)
}

// rawSQL picks the sql entry for the dialect: its own, then mysql for
// MariaDB, then default.
func (g *gen) rawSQL(bodies map[string]string) {
	d := g.d
	s, ok := bodies[d.name]
	if !ok && d == MariaDB {
		s, ok = bodies["mysql"]
	}
	if !ok {
		s, ok = bodies["default"]
	}
	if !ok {
		g.fail("sql has no entry for %s and no default", d)
		return
	}
	g.emit(s)
}

func (g *gen) createTable(t *CreateTable) {
	d, q := g.d, g.d.q
	// SQLite only auto-increments an inline INTEGER PRIMARY KEY.
	inlinePK := false
	var defs []string
	for i := range t.Columns {
		c := &t.Columns[i]
		inlinePK = inlinePK || (c.AutoIncrement && d.family == lite)
		defs = append(defs, g.columnDef(c, slices.Contains(t.PrimaryKey, c.Name)))
	}
	if len(t.PrimaryKey) > 0 && !inlinePK {
		defs = append(defs, "PRIMARY KEY ("+d.qList(t.PrimaryKey)+")")
	}
	for _, u := range t.Uniques {
		defs = append(defs, fmt.Sprintf("CONSTRAINT %s UNIQUE (%s)", q(u.Name), d.qList(u.Columns)))
	}
	if d.family == my { // MySQL can declare indexes inline, which avoids CREATE INDEX IF NOT EXISTS
		for _, ix := range t.Indexes {
			if ix.Where != "" {
				g.fail("%s has no partial indexes (index %s has where)", d, ix.Name)
			}
			kw := "INDEX"
			if ix.Unique {
				kw = "UNIQUE INDEX"
			}
			defs = append(defs, fmt.Sprintf("%s %s (%s)", kw, q(ix.Name), d.qList(ix.Columns)))
		}
	}
	for i := range t.ForeignKeys {
		defs = append(defs, fmt.Sprintf("CONSTRAINT %s %s", q(t.ForeignKeys[i].Name), g.fkClause(&t.ForeignKeys[i])))
	}
	for _, ck := range t.Checks {
		defs = append(defs, fmt.Sprintf("CONSTRAINT %s CHECK (%s)", q(ck.Name), ck.Expr))
	}
	ine := ""
	if t.IfNotExists {
		ine = kwIfNotExists
	}
	g.emit("CREATE TABLE " + ine + q(t.Name) + " (\n  " + strings.Join(defs, ",\n  ") + "\n)")
	if d.family != my {
		for _, ix := range t.Indexes {
			ix.IfNotExists = ix.IfNotExists || t.IfNotExists
			g.createIndex(&ix)
		}
	}
}

func (g *gen) columnDef(c *Column, inPK bool) string {
	d := g.d
	s := d.q(c.Name) + " " + g.colType(c)
	if inPK || (c.Nullable != nil && !*c.Nullable) {
		s += " NOT NULL"
	}
	if def := g.defaultValue(c); def != "" {
		s += " DEFAULT " + def
	}
	if c.AutoIncrement {
		s += [3]string{" GENERATED BY DEFAULT AS IDENTITY", " AUTO_INCREMENT", " PRIMARY KEY AUTOINCREMENT"}[d.family]
	}
	if c.Unique {
		s += " UNIQUE"
	}
	return s
}

func (g *gen) colType(c *Column) string {
	if c.Type == "timestamptz" && g.d.family == my {
		g.warn("%s has no time-zone-aware timestamp; column %s becomes DATETIME (store UTC)", g.d, c.Name)
	}
	return g.d.columnType(c)
}

// defaultValue returns the DEFAULT clause body, or "" for none.
func (g *gen) defaultValue(c *Column) string {
	if c.DefaultExpr != "" {
		switch e := strings.ToUpper(strings.TrimSpace(c.DefaultExpr)); e {
		case "CURRENT_TIMESTAMP", "CURRENT_DATE", "CURRENT_TIME":
			return e
		}
		return "(" + c.DefaultExpr + ")"
	}
	if c.Default == nil {
		return ""
	}
	if g.d == MySQL && slices.Contains([]string{"text", "json", "binary"}, c.Type) {
		g.fail("MySQL doesn't allow a literal default on %s column %s; use defaultExpr", c.Type, c.Name)
	}
	return g.d.literal(c.Default)
}

func (g *gen) addColumn(a *AddColumn) {
	d, c := g.d, &a.Column
	if d.family == lite {
		if c.Unique {
			g.fail("SQLite can't add a UNIQUE column; add it, then createIndex with unique: true")
		}
		if c.DefaultExpr != "" {
			g.fail("SQLite can't add a column with an expression default (%s)", c.DefaultExpr)
		}
		if c.Nullable != nil && !*c.Nullable && c.Default == nil {
			g.fail("SQLite can't add a NOT NULL column without a default")
		}
	}
	g.emit(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s%s", d.q(a.Table), g.ifNotExists(a.IfNotExists), g.columnDef(c, false)))
}

func (g *gen) alterColumn(a *AlterColumn) {
	d, c := g.d, &a.Column
	switch d.family {
	case lite:
		g.rebuildNeeded("alter a column in")
	case my:
		g.emit(fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s", d.q(a.Table), g.columnDef(c, false)))
	default:
		col := "ALTER COLUMN " + d.q(c.Name)
		parts := []string{col + " TYPE " + g.colType(c)}
		if c.Nullable != nil && !*c.Nullable {
			parts = append(parts, col+" SET NOT NULL")
		} else {
			parts = append(parts, col+" DROP NOT NULL")
		}
		if def := g.defaultValue(c); def != "" {
			parts = append(parts, col+" SET DEFAULT "+def)
		} else {
			parts = append(parts, col+" DROP DEFAULT")
		}
		g.emit("ALTER TABLE " + d.q(a.Table) + " " + strings.Join(parts, ", "))
	}
}

func (g *gen) createIndex(ix *Index) {
	d := g.d
	s := "CREATE "
	if ix.Unique {
		s += "UNIQUE "
	}
	s += "INDEX " + g.ifNotExistsIndex(ix.IfNotExists) + d.q(ix.Name) + " ON " + d.q(ix.Table) + " (" + d.qList(ix.Columns) + ")"
	if ix.Where != "" {
		if d.family == my {
			g.fail("%s has no partial indexes (index %s has where)", d, ix.Name)
		}
		s += " WHERE " + ix.Where
	}
	g.emit(s)
}

func (g *gen) fkClause(fk *ForeignKey) string {
	s := fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s (%s)", g.d.qList(fk.Columns), g.d.q(fk.RefTable), g.d.qList(fk.RefColumns))
	if fk.OnDelete != "" {
		s += " ON DELETE " + strings.ToUpper(fk.OnDelete)
	}
	if fk.OnUpdate != "" {
		s += " ON UPDATE " + strings.ToUpper(fk.OnUpdate)
	}
	return s
}

func (g *gen) dropConstraint(dc *DropConstraint) {
	g.emit(fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.d.q(dc.Table), g.d.q(dc.Name)))
}

// rebuildNeeded fails on SQLite, whose ALTER TABLE can't do what was asked.
func (g *gen) rebuildNeeded(what string) bool {
	if g.d.family != lite {
		return false
	}
	g.fail("SQLite can't %s an existing table (it needs a table rebuild); use a sql operation", what)
	return true
}

// ifExists renders IF EXISTS for dropping a table, index or column.
func (g *gen) ifExists(want bool, what string) string {
	if !want {
		return ""
	}
	if (what != "table" && g.d == MySQL) || (what == "column" && g.d == SQLite) {
		g.fail("%s doesn't support DROP %s IF EXISTS", g.d, strings.ToUpper(what))
	}
	return "IF EXISTS "
}

// ifNotExists is for ADD COLUMN, which only Postgres and MariaDB support.
func (g *gen) ifNotExists(want bool) string {
	if !want {
		return ""
	}
	if g.d == MySQL || g.d == SQLite {
		g.fail("%s doesn't support ADD COLUMN IF NOT EXISTS", g.d)
	}
	return kwIfNotExists
}

func (g *gen) ifNotExistsIndex(want bool) string {
	if !want {
		return ""
	}
	if g.d == MySQL {
		g.fail("MySQL doesn't support CREATE INDEX IF NOT EXISTS (MariaDB does)")
	}
	return kwIfNotExists
}

// where renders AND-ed equalities, appending bind args.
func (g *gen) where(w map[string]any, args *[]any) string {
	if len(w) == 0 {
		return ""
	}
	var conds []string
	for _, k := range slices.Sorted(maps.Keys(w)) {
		if w[k] == nil {
			conds = append(conds, g.d.q(k)+" IS NULL")
			continue
		}
		*args = append(*args, g.arg(w[k]))
		conds = append(conds, g.d.q(k)+" = "+g.d.ph(len(*args)))
	}
	return " WHERE " + strings.Join(conds, " AND ")
}

// arg converts nested YAML maps/lists to JSON text so they can fill json columns.
func (g *gen) arg(v any) any {
	if k := reflect.ValueOf(v).Kind(); k != reflect.Map && k != reflect.Slice {
		return v
	}
	b, err := json.Marshal(v)
	if err != nil {
		g.fail("value can't be stored as JSON: %v", err)
		return nil
	}
	return string(b)
}
