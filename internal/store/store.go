package store

import (
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dexsql-tree/dexsql-tree/internal/dex"
	_ "modernc.org/sqlite"
)

var dbSeq int64

// Store is an in-memory, read-only SQL view over one loaded container.
type Store struct {
	DB        *sql.DB
	Container *dex.Container
	Warnings  []string
	Stats     dex.Stats
	path      string
}

// Open parses nothing; it materialises an already-loaded container into an
// in-memory SQLite database. All relations are physical tables, which keeps the
// query surface identical to the documented schema without virtual-table
// extensions.
func Open(c *dex.Container) (*Store, error) {
	dsn := fmt.Sprintf("file:dexsqltree-%d?mode=memory&cache=shared", atomic.AddInt64(&dbSeq, 1))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{DB: db, Container: c, Stats: c.Stats(), Warnings: append([]string{}, c.Warnings...)}
	for _, t := range []string{
		"PRAGMA journal_mode=OFF", "PRAGMA synchronous=OFF", "PRAGMA temp_store=MEMORY",
		"PRAGMA cache_size=-200000", "PRAGMA locking_mode=EXCLUSIVE",
	} {
		if _, err := db.Exec(t); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragmas: %w", err)
		}
	}
	for _, stmt := range DDL() {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("schema: %w (%s)", err, stmt)
		}
	}
	if err := s.load(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the in-memory database.
func (s *Store) Close() error { return s.DB.Close() }

type loader struct {
	tx    *sql.Tx
	stmts map[string]*sql.Stmt
	s     *Store
}

func (s *Store) load() error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	l := &loader{tx: tx, stmts: map[string]*sql.Stmt{}, s: s}
	if err := l.run(); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, idx := range indexDDL {
		if _, err := s.DB.Exec(idx); err != nil {
			return fmt.Errorf("index: %w", err)
		}
	}
	return nil
}

var indexDDL = []string{
	"CREATE INDEX idx_methods_desc ON methods(descriptor)",
	"CREATE INDEX idx_methods_class ON methods(class_descriptor)",
	"CREATE INDEX idx_classes_desc ON classes(descriptor)",
	"CREATE INDEX idx_method_calls_callee ON method_calls(callee)",
	"CREATE INDEX idx_method_calls_method ON method_calls(method)",
	"CREATE INDEX idx_method_strings_value ON method_strings(value)",
	"CREATE INDEX idx_strings_value ON strings(value)",
	"CREATE INDEX idx_fields_desc ON fields(descriptor)",
	"CREATE INDEX idx_method_refs_kind ON method_refs(index_kind, \"index\")",
	"CREATE INDEX idx_class_methods_method ON class_methods(method_idx)",
}

// ins inserts one row into a catalog table, preparing the statement on first
// use. Column order must match the catalog.
func (l *loader) ins(table string, vals ...any) error {
	st, ok := l.stmts[table]
	if !ok {
		n := len(CatalogByName[table].Cols)
		if len(vals) != n {
			return fmt.Errorf("store: %s expects %d columns, got %d", table, n, len(vals))
		}
		q := "INSERT INTO " + quote(table) + " VALUES (" + strings.TrimSuffix(strings.Repeat("?,", n), ",") + ")"
		var err error
		st, err = l.tx.Prepare(q)
		if err != nil {
			return fmt.Errorf("prepare %s: %w", table, err)
		}
		l.stmts[table] = st
	}
	_, err := st.Exec(vals...)
	return err
}

func (l *loader) run() error {
	for _, step := range []func() error{
		l.dexFiles, l.strings, l.types, l.protos, l.fields, l.methods,
		l.classes, l.classMembers, l.codeItems, l.bytecodeRefs,
		l.annotations, l.debugInfo, l.layout,
	} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// Query executes a single read-only statement and returns at most limit rows.
func (s *Store) Query(q string, limit int) (*Result, error) {
	start := time.Now()
	if err := Guard(q); err != nil {
		return nil, err
	}
	wrapped := q
	if limit > 0 {
		wrapped = "SELECT * FROM (" + strings.TrimSuffix(strings.TrimSpace(q), ";") + ") LIMIT " + fmt.Sprint(limit+1)
	}
	rows, err := s.DB.Query(wrapped)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	res := &Result{Columns: cols, RowCount: 0}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, v := range vals {
			switch t := v.(type) {
			case []byte:
				vals[i] = string(t)
			case time.Time:
				vals[i] = t.Format(time.RFC3339Nano)
			}
		}
		res.Rows = append(res.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	res.RowCount = len(res.Rows)
	if limit > 0 && len(res.Rows) > limit {
		res.Rows = res.Rows[:limit]
		res.RowCount = limit
		res.Truncated = true
	}
	res.ElapsedMS = time.Since(start).Milliseconds()
	return res, nil
}

// Result is a JSON-friendly query result.
type Result struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	RowCount  int      `json:"rowCount"`
	Truncated bool     `json:"truncated"`
	ElapsedMS int64    `json:"elapsedMs"`
}

// hasStatementSeparator reports whether a semicolon appears outside string
// literals and comments. Trailing semicolons are stripped by the caller.
func hasStatementSeparator(q string) bool {
	inSingle, inDouble := false, false
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case inSingle:
			if c == '\'' {
				if i+1 < len(q) && q[i+1] == '\'' {
					i++
				} else {
					inSingle = false
				}
			}
		case inDouble:
			if c == '"' {
				inDouble = false
			}
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case c == ';':
			return true
		}
	}
	return false
}

// Guard rejects statements that are not read-only. The database itself is also
// opened with query_only=ON, so this is a friendly early error rather than the
// only line of defence.
func Guard(q string) error {
	s := strings.TrimSpace(q)
	for {
		switch {
		case strings.HasPrefix(s, "--"):
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = strings.TrimSpace(s[i+1:])
				continue
			}
			return fmt.Errorf("empty statement")
		case strings.HasPrefix(s, "/*"):
			if i := strings.Index(s, "*/"); i >= 0 {
				s = strings.TrimSpace(s[i+2:])
				continue
			}
			return fmt.Errorf("unterminated comment")
		}
		break
	}
	if s == "" {
		return fmt.Errorf("empty statement")
	}
	body := strings.TrimSuffix(s, ";")
	if hasStatementSeparator(body) {
		return fmt.Errorf("only one statement is allowed")
	}
	lower := strings.ToLower(body)
	for _, p := range []string{"select", "with", "values", "explain"} {
		if strings.HasPrefix(lower, p) {
			return nil
		}
	}
	return fmt.Errorf("read-only interface: statement must start with SELECT, WITH, VALUES or EXPLAIN")
}
