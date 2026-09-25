// Package polyschema applies portable YAML/JSON migration files to
// PostgreSQL, MySQL, MariaDB and SQLite.
package polyschema

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FormatVersion is the migration file format understood by this package.
const FormatVersion = 1

// fileRe matches migration filenames: a numeric version, an optional
// "_description" or "-description", and a .yml/.yaml/.json extension.
var fileRe = regexp.MustCompile(`^(\d+)([_-].*)?\.(ya?ml|json)$`)

// File is one parsed migration file.
type File struct {
	Name        string      `yaml:"-"` // base filename
	Version     int64       `yaml:"-"` // numeric filename prefix
	Checksum    string      `yaml:"-"` // sha256 of the raw file, hex
	Format      int         `yaml:"format"`
	Transaction *bool       `yaml:"transaction"`
	Operations  []Operation `yaml:"operations"`
}

// UseTransaction reports whether the file runs inside a transaction (the default).
func (f *File) UseTransaction() bool { return f.Transaction == nil || *f.Transaction }

// Operation holds exactly one non-nil field.
type Operation struct {
	CreateTable    *CreateTable      `yaml:"createTable"`
	DropTable      *DropTable        `yaml:"dropTable"`
	RenameTable    *RenameTable      `yaml:"renameTable"`
	AddColumn      *AddColumn        `yaml:"addColumn"`
	DropColumn     *DropColumn       `yaml:"dropColumn"`
	RenameColumn   *RenameColumn     `yaml:"renameColumn"`
	AlterColumn    *AlterColumn      `yaml:"alterColumn"`
	CreateIndex    *Index            `yaml:"createIndex"`
	DropIndex      *DropIndex        `yaml:"dropIndex"`
	AddUnique      *Unique           `yaml:"addUnique"`
	DropUnique     *DropConstraint   `yaml:"dropUnique"`
	AddForeignKey  *ForeignKey       `yaml:"addForeignKey"`
	DropForeignKey *DropConstraint   `yaml:"dropForeignKey"`
	AddCheck       *CheckConstraint  `yaml:"addCheck"`
	DropCheck      *DropConstraint   `yaml:"dropCheck"`
	Insert         *Insert           `yaml:"insert"`
	Update         *Update           `yaml:"update"`
	Delete         *Delete           `yaml:"delete"`
	SQL            map[string]string `yaml:"sql"`
}

type Column struct {
	Name          string `yaml:"name"`
	Type          string `yaml:"type"`
	Length        int    `yaml:"length"`
	Precision     int    `yaml:"precision"`
	Scale         int    `yaml:"scale"`
	Nullable      *bool  `yaml:"nullable"` // nil = nullable, unless part of the primary key
	PrimaryKey    bool   `yaml:"primaryKey"`
	AutoIncrement bool   `yaml:"autoIncrement"`
	Unique        bool   `yaml:"unique"`
	Default       any    `yaml:"default"`     // literal value
	DefaultExpr   string `yaml:"defaultExpr"` // raw SQL expression, e.g. CURRENT_TIMESTAMP
}

type CreateTable struct {
	Name        string            `yaml:"name"`
	IfNotExists bool              `yaml:"ifNotExists"`
	Columns     []Column          `yaml:"columns"`
	PrimaryKey  []string          `yaml:"primaryKey"`
	Indexes     []Index           `yaml:"indexes"`
	Uniques     []Unique          `yaml:"uniques"`
	ForeignKeys []ForeignKey      `yaml:"foreignKeys"`
	Checks      []CheckConstraint `yaml:"checks"`
}

type DropTable struct {
	Name     string `yaml:"name"`
	IfExists bool   `yaml:"ifExists"`
	Cascade  bool   `yaml:"cascade"`
}

type RenameTable struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

type AddColumn struct {
	Table       string `yaml:"table"`
	IfNotExists bool   `yaml:"ifNotExists"`
	Column      Column `yaml:"column"`
}

type DropColumn struct {
	Table    string `yaml:"table"`
	Name     string `yaml:"name"`
	IfExists bool   `yaml:"ifExists"`
}

type RenameColumn struct {
	Table string `yaml:"table"`
	From  string `yaml:"from"`
	To    string `yaml:"to"`
}

// AlterColumn restates the column's full definition: type, nullability and
// default all end up exactly as given (an omitted default drops it).
type AlterColumn struct {
	Table  string `yaml:"table"`
	Column `yaml:",inline"`
}

type Index struct {
	Table       string   `yaml:"table"`
	Name        string   `yaml:"name"`
	Columns     []string `yaml:"columns"`
	Unique      bool     `yaml:"unique"`
	IfNotExists bool     `yaml:"ifNotExists"`
	Where       string   `yaml:"where"` // partial index predicate, raw SQL
}

type DropIndex struct {
	Table    string `yaml:"table"`
	Name     string `yaml:"name"`
	IfExists bool   `yaml:"ifExists"`
}

type Unique struct {
	Table   string   `yaml:"table"`
	Name    string   `yaml:"name"`
	Columns []string `yaml:"columns"`
}

type ForeignKey struct {
	Table      string   `yaml:"table"`
	Name       string   `yaml:"name"`
	Columns    []string `yaml:"columns"`
	RefTable   string   `yaml:"refTable"`
	RefColumns []string `yaml:"refColumns"`
	OnDelete   string   `yaml:"onDelete"`
	OnUpdate   string   `yaml:"onUpdate"`
}

type CheckConstraint struct {
	Table string `yaml:"table"`
	Name  string `yaml:"name"`
	Expr  string `yaml:"expr"` // raw SQL
}

type DropConstraint struct {
	Table string `yaml:"table"`
	Name  string `yaml:"name"`
}

type Insert struct {
	Table string           `yaml:"table"`
	Rows  []map[string]any `yaml:"rows"`
}

// Update sets columns on rows matching every Where equality (null = IS NULL).
// All must be true to update every row.
type Update struct {
	Table string         `yaml:"table"`
	Set   map[string]any `yaml:"set"`
	Where map[string]any `yaml:"where"`
	All   bool           `yaml:"all"`
}

type Delete struct {
	Table string         `yaml:"table"`
	Where map[string]any `yaml:"where"`
	All   bool           `yaml:"all"`
}

// Parse decodes a migration file. name supplies the version and must match
// NNNN_description.(yml|yaml|json).
func Parse(name string, data []byte) (*File, error) {
	base := path.Base(name)
	m := fileRe.FindStringSubmatch(base)
	if m == nil {
		return nil, fmt.Errorf("%s: filename must look like 0001_description.yml (or .yaml/.json)", base)
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s: bad version: %w", base, err)
	}
	f := &File{Name: base, Version: v, Format: FormatVersion}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(f); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", base, err)
	}
	if f.Format != FormatVersion {
		return nil, fmt.Errorf("%s: unsupported format %d (this package reads format %d)", base, f.Format, FormatVersion)
	}
	if len(f.Operations) == 0 {
		return nil, fmt.Errorf("%s: no operations", base)
	}
	for i := range f.Operations {
		if err := f.Operations[i].validate(); err != nil {
			return nil, fmt.Errorf("%s: operation %d: %w", base, i+1, err)
		}
	}
	sum := sha256.Sum256(data)
	f.Checksum = hex.EncodeToString(sum[:])
	return f, nil
}

func (o *Operation) validate() error {
	v := reflect.ValueOf(o).Elem()
	all := make([]string, 0, v.NumField())
	var set []string
	for i := range v.NumField() {
		tag := v.Type().Field(i).Tag.Get("yaml")
		all = append(all, tag)
		if !v.Field(i).IsNil() {
			set = append(set, tag)
		}
	}
	if len(set) != 1 {
		return fmt.Errorf("needs exactly one of %s (got %d)", strings.Join(all, ", "), len(set))
	}
	switch {
	case o.CreateTable != nil:
		return o.CreateTable.validate()
	case o.DropTable != nil:
		return need("dropTable.name", o.DropTable.Name)
	case o.RenameTable != nil:
		return need("renameTable.from", o.RenameTable.From, "renameTable.to", o.RenameTable.To)
	case o.AddColumn != nil:
		return o.AddColumn.validate()
	case o.DropColumn != nil:
		return need("dropColumn.table", o.DropColumn.Table, "dropColumn.name", o.DropColumn.Name)
	case o.RenameColumn != nil:
		r := o.RenameColumn
		return need("renameColumn.table", r.Table, "renameColumn.from", r.From, "renameColumn.to", r.To)
	case o.AlterColumn != nil:
		return o.AlterColumn.validate()
	case o.CreateIndex != nil:
		return o.CreateIndex.validate()
	case o.DropIndex != nil:
		return need("dropIndex.table", o.DropIndex.Table, "dropIndex.name", o.DropIndex.Name)
	case o.AddUnique != nil:
		return o.AddUnique.validate()
	case o.AddForeignKey != nil:
		return o.AddForeignKey.validate()
	case o.AddCheck != nil:
		return o.AddCheck.validate()
	case o.DropUnique != nil:
		return o.DropUnique.validate()
	case o.DropForeignKey != nil:
		return o.DropForeignKey.validate()
	case o.DropCheck != nil:
		return o.DropCheck.validate()
	case o.Insert != nil:
		return o.Insert.validate()
	case o.Update != nil:
		return o.Update.validate()
	case o.Delete != nil:
		if err := need("delete.table", o.Delete.Table); err != nil {
			return err
		}
		return whereOrAll("delete", o.Delete.Where, o.Delete.All)
	default:
		return validateSQL(o.SQL)
	}
}

func (a *AddColumn) validate() error {
	if err := need("addColumn.table", a.Table); err != nil {
		return err
	}
	if a.Column.PrimaryKey || a.Column.AutoIncrement {
		return errors.New("addColumn: a new column can't be primaryKey or autoIncrement")
	}
	return a.Column.validate()
}

func (a *AlterColumn) validate() error {
	if err := need("alterColumn.table", a.Table); err != nil {
		return err
	}
	if a.PrimaryKey || a.AutoIncrement || a.Unique {
		return errors.New("alterColumn: primaryKey, autoIncrement and unique can't be changed here; use addUnique or a new table")
	}
	return a.Column.validate()
}

func (ins *Insert) validate() error {
	if err := need("insert.table", ins.Table); err != nil {
		return err
	}
	if len(ins.Rows) == 0 {
		return errors.New("insert.rows is required")
	}
	for i, r := range ins.Rows {
		if len(r) == 0 {
			return fmt.Errorf("insert.rows[%d] is empty", i)
		}
	}
	return nil
}

func (u *Update) validate() error {
	if err := need("update.table", u.Table); err != nil {
		return err
	}
	if len(u.Set) == 0 {
		return errors.New("update.set is required")
	}
	return whereOrAll("update", u.Where, u.All)
}

func validateSQL(bodies map[string]string) error {
	if len(bodies) == 0 {
		return errors.New("sql needs at least one of postgres, mysql, mariadb, sqlite, default")
	}
	for k := range bodies {
		if !slices.Contains([]string{"postgres", "mysql", "mariadb", "sqlite", "default"}, k) {
			return fmt.Errorf("sql: unknown engine %q", k)
		}
	}
	return nil
}

func (t *CreateTable) validate() error {
	if err := need("createTable.name", t.Name); err != nil {
		return err
	}
	if len(t.Columns) == 0 {
		return errors.New("createTable.columns is required")
	}
	var names, colPK []string
	for i := range t.Columns {
		c := &t.Columns[i]
		if err := c.validate(); err != nil {
			return err
		}
		if slices.Contains(names, c.Name) {
			return fmt.Errorf("duplicate column %q", c.Name)
		}
		names = append(names, c.Name)
		if c.PrimaryKey {
			colPK = append(colPK, c.Name)
		}
	}
	if len(colPK) > 0 && len(t.PrimaryKey) > 0 {
		return errors.New("set primaryKey on the columns or on the table, not both")
	}
	if len(t.PrimaryKey) == 0 {
		t.PrimaryKey = colPK
	}
	if err := hasCols(names, "primaryKey", t.PrimaryKey); err != nil {
		return err
	}
	for _, c := range t.Columns {
		if c.AutoIncrement && (len(t.PrimaryKey) != 1 || t.PrimaryKey[0] != c.Name) {
			return fmt.Errorf("column %q: autoIncrement must be the only primary key column", c.Name)
		}
	}
	return t.validateNested(names)
}

// validateNested checks indexes, uniques, foreign keys and checks, which
// inherit the table name.
func (t *CreateTable) validateNested(names []string) error {
	has := func(what string, cols []string) error { return hasCols(names, what, cols) }
	own := func(what, table string) error {
		if table != "" && table != t.Name {
			return fmt.Errorf("%s belongs to table %q, not %q", what, table, t.Name)
		}
		return nil
	}
	for i := range t.Indexes {
		ix := &t.Indexes[i]
		if err := own("index "+ix.Name, ix.Table); err != nil {
			return err
		}
		ix.Table = t.Name
		if err := ix.validate(); err != nil {
			return err
		}
		if err := has("index "+ix.Name, ix.Columns); err != nil {
			return err
		}
	}
	for i := range t.Uniques {
		u := &t.Uniques[i]
		if err := own("unique "+u.Name, u.Table); err != nil {
			return err
		}
		u.Table = t.Name
		if err := u.validate(); err != nil {
			return err
		}
		if err := has("unique "+u.Name, u.Columns); err != nil {
			return err
		}
	}
	for i := range t.ForeignKeys {
		fk := &t.ForeignKeys[i]
		if err := own("foreign key "+fk.Name, fk.Table); err != nil {
			return err
		}
		fk.Table = t.Name
		if err := fk.validate(); err != nil {
			return err
		}
		if err := has("foreign key "+fk.Name, fk.Columns); err != nil {
			return err
		}
	}
	for i := range t.Checks {
		ck := &t.Checks[i]
		if err := own("check "+ck.Name, ck.Table); err != nil {
			return err
		}
		ck.Table = t.Name
		if err := ck.validate(); err != nil {
			return err
		}
	}
	return nil
}

// hasCols reports the first of cols that isn't in names.
func hasCols(names []string, what string, cols []string) error {
	for _, c := range cols {
		if !slices.Contains(names, c) {
			return fmt.Errorf("%s: unknown column %q", what, c)
		}
	}
	return nil
}

func (c *Column) validate() error {
	if err := need("column.name", c.Name); err != nil {
		return err
	}
	if _, ok := typeNames[c.Type]; !ok {
		return fmt.Errorf("column %q: unknown type %q (use one of %s)", c.Name, c.Type, strings.Join(typeList(), ", "))
	}
	switch {
	case c.Type == "string" && c.Length == 0: //nolint:goconst // portable type name
		c.Length = 255
	case c.Type == "decimal" && (c.Precision <= 0 || c.Scale < 0 || c.Scale > c.Precision):
		return fmt.Errorf("column %q: decimal needs precision > 0 and 0 <= scale <= precision", c.Name)
	case c.Length < 0:
		return fmt.Errorf("column %q: negative length", c.Name)
	}
	if c.AutoIncrement && !slices.Contains([]string{"smallint", "integer", "bigint"}, c.Type) { //nolint:goconst // portable type names
		return fmt.Errorf("column %q: autoIncrement needs an integer type", c.Name)
	}
	if c.Default != nil && c.DefaultExpr != "" {
		return fmt.Errorf("column %q: set default or defaultExpr, not both", c.Name)
	}
	if c.AutoIncrement && (c.Default != nil || c.DefaultExpr != "") {
		return fmt.Errorf("column %q: autoIncrement columns can't have a default", c.Name)
	}
	switch c.Default.(type) {
	case nil, bool, int, int64, uint64, float64, string:
	default:
		return fmt.Errorf("column %q: default must be a string, number or boolean (quote dates)", c.Name)
	}
	return nil
}

func (ix *Index) validate() error {
	if err := need("index.table", ix.Table, "index.name", ix.Name); err != nil {
		return err
	}
	return needCols("index "+ix.Name, ix.Columns)
}

func (u *Unique) validate() error {
	if err := need("unique.table", u.Table, "unique.name", u.Name); err != nil {
		return err
	}
	return needCols("unique "+u.Name, u.Columns)
}

var fkActions = []string{"cascade", "restrict", "set null", "set default", "no action"}

func (fk *ForeignKey) validate() error {
	if err := need("foreignKey.table", fk.Table, "foreignKey.name", fk.Name, "foreignKey.refTable", fk.RefTable); err != nil {
		return err
	}
	if err := needCols("foreign key "+fk.Name, fk.Columns); err != nil {
		return err
	}
	if len(fk.RefColumns) != len(fk.Columns) {
		return fmt.Errorf("foreign key %s: columns and refColumns must be the same length", fk.Name)
	}
	for _, a := range []string{fk.OnDelete, fk.OnUpdate} {
		if a != "" && !slices.Contains(fkActions, strings.ToLower(a)) {
			return fmt.Errorf("foreign key %s: action %q must be one of %s", fk.Name, a, strings.Join(fkActions, ", "))
		}
	}
	return nil
}

func (ck *CheckConstraint) validate() error {
	return need("check.table", ck.Table, "check.name", ck.Name, "check.expr", ck.Expr)
}

func (dc *DropConstraint) validate() error {
	return need("table", dc.Table, "name", dc.Name)
}

// need takes label, value pairs and reports the first empty value.
func need(pairs ...string) error {
	for i := 0; i+1 < len(pairs); i += 2 {
		if strings.TrimSpace(pairs[i+1]) == "" {
			return fmt.Errorf("%s is required", pairs[i])
		}
	}
	return nil
}

func needCols(what string, cols []string) error {
	if len(cols) == 0 {
		return fmt.Errorf("%s: columns is required", what)
	}
	return nil
}

func whereOrAll(op string, where map[string]any, all bool) error {
	if len(where) == 0 && !all {
		return fmt.Errorf("%s needs where, or all: true to touch every row", op)
	}
	if len(where) > 0 && all {
		return fmt.Errorf("%s: set where or all, not both", op)
	}
	return nil
}
