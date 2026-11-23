package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/byusi-studio/dexsql-tree/internal/cache"
	"github.com/byusi-studio/dexsql-tree/internal/dex"
	_ "modernc.org/sqlite"
)

var dbSeq int64

// Store is an in-memory, read-only SQL view over one loaded container.
type Store struct {
	DB        *sql.DB
	Container *dex.Container
	Warnings  []string
	Stats     dex.Stats
	// Cached reports that this catalog was loaded from the on-disk cache
	// instead of being materialised in this process.
	Cached bool
	// Profile records which materialisation profile produced the catalog.
	Profile Profile
	path    string
}

// Profile selects how much of the DEX model is written to SQL. "fast" skips the
// annotation tables, which are the largest and least frequently queried part of
// the catalog; the tables still exist and simply stay empty.
type Profile string

const (
	// ProfileFast materialises entities, references, bytecode and call sites.
	ProfileFast Profile = "fast"
	// ProfileFull additionally materialises annotations and static values.
	ProfileFull Profile = "full"
)

// ParseProfile validates a user-supplied profile name.
func ParseProfile(s string) (Profile, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "fast", "default", "slim":
		return ProfileFast, nil
	case "full", "all", "annotations":
		return ProfileFull, nil
	}
	return "", fmt.Errorf("unknown profile %q (use fast or full)", s)
}

// Options selects where the materialised catalog lives. The default in-memory
// database is fastest for small and medium inputs; Disk trades a temporary file
// for a much smaller resident set, which matters for multi-dex APKs.
type Options struct {
	Disk    bool
	CacheMB int
	TempDir string
	// Profile defaults to ProfileFast.
	Profile Profile
	// CacheDir and CacheKey enable the persistent catalog cache. Both must be
	// set; CacheKey normally comes from cache.Key.
	CacheDir string
	CacheKey string
}

// Open materialises an already-loaded container into a read-only SQLite view.
// All relations are physical tables, which keeps the query surface identical to
// the documented schema without virtual-table extensions.
//
// When CacheDir and CacheKey are set the catalog is written to
// <CacheDir>/<CacheKey>.db and a later call with the same key mounts that file
// read-only, skipping materialisation entirely.
func Open(c *dex.Container, opts ...Options) (*Store, error) {
	var opt Options
	if len(opts) > 0 {
		opt = opts[0]
	}
	profile := opt.Profile
	if profile == "" {
		profile = ProfileFast
	}
	cachePath := ""
	if opt.CacheDir != "" && opt.CacheKey != "" {
		cachePath = cache.Path(opt.CacheDir, opt.CacheKey)
		if _, ok := cache.Stat(opt.CacheDir, opt.CacheKey); ok {
			if s, err := openReadOnly(c, cachePath, profile, true); err == nil {
				return s, nil
			}
			// A half-written or corrupt catalog must not be served twice.
			os.Remove(cachePath)
		}
		if err := cache.Ensure(opt.CacheDir); err != nil {
			return nil, err
		}
		cache.CleanStale(opt.CacheDir, time.Hour)
	}
	seq := atomic.AddInt64(&dbSeq, 1)
	dsn := fmt.Sprintf("file:dexsqltree-%d?mode=memory&cache=shared", seq)
	tmpPath := ""
	writePath := ""
	if cachePath != "" {
		writePath = fmt.Sprintf("%s.tmp%d", cachePath, os.Getpid())
		tmpPath = writePath
		dsn = "file:" + filepath.ToSlash(writePath) + "?mode=rwc"
	} else if opt.Disk {
		dir := opt.TempDir
		if dir == "" {
			dir = os.TempDir()
		}
		tmpPath = filepath.Join(dir, fmt.Sprintf("dexsqltree-%d-%d.db", os.Getpid(), seq))
		dsn = "file:" + filepath.ToSlash(tmpPath) + "?mode=rwc"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{DB: db, Container: c, Stats: c.Stats(), Warnings: append([]string{}, c.Warnings...), path: tmpPath, Profile: profile}
	fail := func(err error) (*Store, error) {
		db.Close()
		s.removeFiles()
		return nil, err
	}
	cacheKB := -200000
	if opt.Disk || cachePath != "" {
		cacheKB = -65536
	}
	if opt.CacheMB > 0 {
		cacheKB = -opt.CacheMB * 1024
	}
	pragmas := []string{
		"PRAGMA journal_mode=OFF", "PRAGMA synchronous=OFF",
		fmt.Sprintf("PRAGMA cache_size=%d", cacheKB), "PRAGMA locking_mode=EXCLUSIVE",
	}
	if opt.Disk || cachePath != "" {
		pragmas = append(pragmas, "PRAGMA temp_store=FILE")
	} else {
		pragmas = append(pragmas, "PRAGMA temp_store=MEMORY")
	}
	for _, t := range pragmas {
		if _, err := db.Exec(t); err != nil {
			return fail(fmt.Errorf("pragmas: %w", err))
		}
	}
	for _, stmt := range DDL() {
		if _, err := db.Exec(stmt); err != nil {
			return fail(fmt.Errorf("schema: %w (%s)", err, stmt))
		}
	}
	if err := s.load(profile); err != nil {
		return fail(err)
	}
	for _, stmt := range viewDDL {
		if _, err := db.Exec(stmt); err != nil {
			return fail(fmt.Errorf("view: %w (%s)", err, stmt))
		}
	}
	if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
		return fail(err)
	}
	if cachePath != "" {
		// Close first so the file is complete on disk, then publish it and
		// reopen read-only. Publishing by rename keeps readers from ever seeing
		// a partially written catalog.
		if err := db.Close(); err != nil {
			os.Remove(writePath)
			return nil, err
		}
		s.path = ""
		if err := os.Rename(writePath, cachePath); err != nil {
			os.Remove(writePath)
			return nil, err
		}
		return openReadOnly(c, cachePath, profile, false)
	}
	return s, nil
}

// openReadOnly mounts an existing catalog. The container is still parsed by the
// caller, so call trees and smali keep working; only materialisation is skipped.
func openReadOnly(c *dex.Container, path string, profile Profile, cached bool) (*Store, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&cache=private"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, p := range []string{"PRAGMA cache_size=-65536", "PRAGMA temp_store=FILE", "PRAGMA query_only=ON"} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, err
		}
	}
	// A short or foreign file is opened by SQLite as an empty database, so the
	// only reliable test is that the catalog tables are actually there.
	var n int
	if err := db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='methods'`).Scan(&n); err != nil || n == 0 {
		db.Close()
		return nil, fmt.Errorf("catalog %s is not usable", path)
	}
	return &Store{
		DB:        db,
		Container: c,
		Stats:     c.Stats(),
		Warnings:  append([]string{}, c.Warnings...),
		Cached:    cached,
		Profile:   profile,
	}, nil
}

// Close releases the database and removes the temporary file, if any.
func (s *Store) Close() error {
	err := s.DB.Close()
	s.removeFiles()
	return err
}

func (s *Store) removeFiles() {
	if s.path == "" {
		return
	}
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		os.Remove(s.path + suffix)
	}
}

type loader struct {
	tx       *sql.Tx
	stmts    map[string]*sql.Stmt
	s        *Store
	profile  Profile
	batches  map[string]*tableBatch
	batchMax map[string]int
}

// tableBatch groups rows into a multi-row INSERT. A benchmark on the pure-Go
// driver shows a shallow optimum: 8 rows per statement is ~27% faster than one
// row per statement, while 128 rows is slower than no batching at all, because
// every bound parameter costs a reflection hop. The width is therefore capped
// so a statement never exceeds 80 parameters.
type tableBatch struct {
	width int
	rows  []any
	count int
	stmts map[int]*sql.Stmt
}

const batchMaxParams = 80

func (b *tableBatch) stmt(tx *sql.Tx, table string, n int) (*sql.Stmt, error) {
	if b.stmts == nil {
		b.stmts = map[int]*sql.Stmt{}
	}
	if st, ok := b.stmts[n]; ok {
		return st, nil
	}
	row := "(" + strings.TrimSuffix(strings.Repeat("?,", b.width), ",") + ")"
	var sb strings.Builder
	sb.WriteString("INSERT INTO " + quote(table) + " VALUES ")
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(row)
	}
	st, err := tx.Prepare(sb.String())
	if err != nil {
		return nil, fmt.Errorf("prepare %s x%d: %w", table, n, err)
	}
	b.stmts[n] = st
	return st, nil
}

// ins inserts one row into a catalog table, preparing the statement on first
// use. Column order must match the catalog.
//
// Multi-row INSERT was measured on an 11 MB APK and was consistently slower:
// the pure-Go driver re-binds every parameter of a wide statement, so a fixed
// 3000-parameter batch cost twice what one row per Exec costs. One prepared
// single-row statement per table stays the fastest option here.
func (l *loader) ins(table string, vals ...any) error {
	b := l.batches[table]
	if b == nil {
		n := len(CatalogByName[table].Cols)
		if len(vals) != n {
			return fmt.Errorf("store: %s expects %d columns, got %d", table, n, len(vals))
		}
		max := batchMaxParams / n
		if max < 1 {
			max = 1
		}
		b = &tableBatch{width: n}
		b.stmts = map[int]*sql.Stmt{}
		l.batches[table] = b
		l.batchMax[table] = max
	}
	if len(vals) != b.width {
		return fmt.Errorf("store: %s expects %d columns, got %d", table, b.width, len(vals))
	}
	b.rows = append(b.rows, vals...)
	b.count++
	if b.count == l.batchMax[table] {
		return l.writeBatch(table, b)
	}
	return nil
}

func (l *loader) writeBatch(table string, b *tableBatch) error {
	if b.count == 0 {
		return nil
	}
	st, err := b.stmt(l.tx, table, b.count)
	if err != nil {
		return err
	}
	if _, err := st.Exec(b.rows...); err != nil {
		return fmt.Errorf("insert %s: %w", table, err)
	}
	b.rows = b.rows[:0]
	b.count = 0
	return nil
}

// flush writes the partial batch of every table.
func (l *loader) flush() error {
	for table, b := range l.batches {
		if err := l.writeBatch(table, b); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) load(profile Profile) error {
	debug := os.Getenv("DEXSQL_DEBUG_LOAD") != ""

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	l := &loader{tx: tx, stmts: map[string]*sql.Stmt{}, s: s, profile: profile,
		batches: map[string]*tableBatch{}, batchMax: map[string]int{}}
	tLoad := time.Now()
	if err := l.run(); err != nil {
		tx.Rollback()
		return err
	}
	if err := l.flush(); err != nil {
		tx.Rollback()
		return err
	}
	if debug {
		fmt.Fprintf(os.Stderr, "load rows: %v\n", time.Since(tLoad))
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	tIdx := time.Now()
	for _, idx := range indexDDL {
		if _, err := s.DB.Exec(idx); err != nil {
			return fmt.Errorf("index: %w", err)
		}
	}
	if debug {
		fmt.Fprintf(os.Stderr, "indexes: %v\n", time.Since(tIdx))
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
	"CREATE INDEX idx_types_class ON types(file_id, descriptor)",
}

func (l *loader) run() error {
	type step struct {
		name string
		fn   func() error
	}
	steps := []step{
		{"dexFiles", l.dexFiles}, {"strings", l.strings}, {"types", l.types},
		{"protos", l.protos}, {"fields", l.fields}, {"methods", l.methods},
		{"classes", l.classes}, {"classMembers", l.classMembers},
		{"codeItems", l.codeItems}, {"bytecodeRefs", l.bytecodeRefs},
		{"annotations", l.annotations}, {"debugInfo", l.debugInfo},
		{"hiddenApi", l.hiddenApi}, {"layout", l.layout},
	}
	debug := os.Getenv("DEXSQL_DEBUG_LOAD") != ""
	for _, st := range steps {
		t := time.Now()
		if err := st.fn(); err != nil {
			return err
		}
		if debug {
			fmt.Fprintf(os.Stderr, "  %-14s %v\n", st.name, time.Since(t))
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
