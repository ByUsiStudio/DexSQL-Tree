package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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

	// built records which tables hold data. A table can exist and be empty —
	// annotations under the fast profile — so sqlite_master alone cannot tell
	// whether a relation was materialised. The list is persisted in _meta so a
	// reopened catalog knows exactly what it may answer.
	built map[string]bool
	// lazy keeps the database writable so EnsureTables can add relations as
	// queries arrive. Long-lived servers use it; a one-shot CLI query does not.
	lazy bool
	mu   sync.Mutex
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
	// Tables restricts materialisation to these relations (tables or views).
	// nil means "whatever the profile builds by default"; an empty non-nil
	// slice means "no table data at all", which is the right plan for
	// SELECT 1 and other statements that read no relation.
	Tables []string
	// Lazy opens an empty, writable catalog and materialises tables when a
	// query first touches them. It is meant for long-lived HTTP/MCP servers,
	// where the statements are not known at open time.
	Lazy bool
}

// Open materialises an already-loaded container into a read-only SQLite view.
// All relations are physical tables, which keeps the query surface identical to
// the documented schema without virtual-table extensions.
//
// When CacheDir and CacheKey are set the catalog is written to
// <CacheDir>/<CacheKey>.db and a later call with the same key mounts that file
// read-only, skipping materialisation entirely.
//
// Options.Tables narrows the build to the relations a statement actually reads.
// The plan is part of the cache identity (cache.Subkey), and a catalog built
// from the profile default can answer any subset of it, so the first restricted
// query still reuses a full catalog when one exists.
func Open(c *dex.Container, opts ...Options) (*Store, error) {
	var opt Options
	if len(opts) > 0 {
		opt = opts[0]
	}
	profile := opt.Profile
	if profile == "" {
		profile = ProfileFast
	}
	p := newPlan(opt.Tables, profile)
	if opt.Lazy {
		// Nothing is built until a statement names it.
		p = newPlan([]string{}, profile)
	}
	defaultPlan := p.need == nil || sameStrings(p.names, DefaultTables(profile))

	baseKey := opt.CacheKey
	if opt.Lazy && profile != ProfileFull {
		// A lazily opened catalog may be asked for any table later, and a
		// read-only cache file cannot grow. A fast catalog leaves the derived
		// tables out, so mounting it would turn a legal query into an error;
		// only a full catalog covers everything a server can ask for.
		baseKey = ""
	}
	key := baseKey
	if !defaultPlan && key != "" && !opt.Lazy {
		if len(p.names) == 0 {
			// Nothing to materialise: SELECT 1 and friends must not leave an
			// empty catalog behind.
			key = ""
		} else {
			key = cache.Subkey(baseKey, p.names)
		}
	}
	cachePath := ""
	if opt.CacheDir != "" && key != "" {
		// A catalog built from the profile default answers every subset of it,
		// so a restricted query looks there first. Derived tables are the
		// exception: a fast default leaves them empty, so a statement naming
		// method_refs must not be served from it.
		candidates := []string{key}
		if !defaultPlan && subsetOfDefault(p.names, profile) && baseKey != "" {
			candidates = append([]string{baseKey}, candidates...)
		}
		seen := map[string]bool{}
		for _, k := range candidates {
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			if _, ok := cache.Stat(opt.CacheDir, k); !ok {
				continue
			}
			path := cache.Path(opt.CacheDir, k)
			if s, err := openReadOnly(c, path, profile, p, true); err == nil {
				return s, nil
			}
			// A half-written, corrupt or wrongly-keyed catalog must not be
			// served twice.
			os.Remove(path)
		}
		if err := cache.Ensure(opt.CacheDir); err != nil {
			return nil, err
		}
		cache.CleanStale(opt.CacheDir, time.Hour)
		cachePath = cache.Path(opt.CacheDir, key)
	}
	if opt.Lazy {
		// A server query arrives after open, so its table set has no stable
		// identity yet; a cache hit above already returned.
		cachePath = ""
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
	s := &Store{
		DB: db, Container: c, Stats: c.Stats(), Warnings: append([]string{}, c.Warnings...),
		path: tmpPath, Profile: profile, lazy: opt.Lazy, built: map[string]bool{},
	}
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
	// page_size only takes effect before the first table exists. 16 KiB pages
	// cut the B-tree depth of the row-count-heavy tables (method_calls,
	// method_strings) and leave a smaller file than the 4 KiB default.
	pragmas := []string{
		"PRAGMA page_size=16384",
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
	if _, err := db.Exec(metaDDL); err != nil {
		return fail(fmt.Errorf("meta: %w", err))
	}
	if err := s.load(profile, p); err != nil {
		return fail(err)
	}
	for _, stmt := range viewDDL {
		if _, err := db.Exec(stmt); err != nil {
			return fail(fmt.Errorf("view: %w (%s)", err, stmt))
		}
	}
	if !s.lazy {
		if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
			return fail(err)
		}
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
		return openReadOnly(c, cachePath, profile, p, false)
	}
	return s, nil
}

// metaDDL records which tables hold data. sqlite_master cannot answer that:
// the fast profile creates every table but leaves the annotation tables empty,
// so an empty table may be either "not built" or "built and empty".
const metaDDL = `CREATE TABLE IF NOT EXISTS _meta (key TEXT PRIMARY KEY, value TEXT);`

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// openReadOnly mounts an existing catalog. The container is still parsed by the
// caller, so call trees and smali keep working; only materialisation is skipped.
//
// A restricted plan is only served when the file actually contains every table
// it needs; otherwise the caller rebuilds, because a missing table would turn
// "no rows" into a wrong answer instead of an error.
func openReadOnly(c *dex.Container, path string, profile Profile, p plan, cached bool) (*Store, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&cache=private"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, pragma := range []string{"PRAGMA cache_size=-65536", "PRAGMA temp_store=FILE", "PRAGMA query_only=ON"} {
		if _, err := db.Exec(pragma); err != nil {
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
	s := &Store{
		DB:        db,
		Container: c,
		Stats:     c.Stats(),
		Warnings:  append([]string{}, c.Warnings...),
		Cached:    cached,
		Profile:   profile,
		built:     map[string]bool{},
	}
	if err := s.readMeta(DefaultTables(profile)); err != nil {
		db.Close()
		return nil, fmt.Errorf("catalog %s is not usable: %w", path, err)
	}
	if missing := p.missing(s.built); len(missing) > 0 {
		db.Close()
		return nil, fmt.Errorf("catalog %s lacks %s", path, strings.Join(missing, ", "))
	}
	return s, nil
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
	tx      *sql.Tx
	s       *Store
	profile Profile
	p       plan
	batches map[string]*tableBatch
}

// tableBatch groups rows into a multi-row INSERT. A benchmark on the pure-Go
// driver shows a shallow optimum: 8 rows per statement is ~27% faster than one
// row per statement, while 128 rows is slower than no batching at all, because
// every bound parameter costs a reflection hop. The width is therefore capped
// so a statement never exceeds 80 parameters.
type tableBatch struct {
	table string
	width int
	max   int
	rows  []any
	count int
	stmts map[int]*sql.Stmt
}

const batchMaxParams = 80

func (b *tableBatch) stmt(tx *sql.Tx, n int) (*sql.Stmt, error) {
	if b.stmts == nil {
		b.stmts = map[int]*sql.Stmt{}
	}
	if st, ok := b.stmts[n]; ok {
		return st, nil
	}
	row := "(" + strings.TrimSuffix(strings.Repeat("?,", b.width), ",") + ")"
	var sb strings.Builder
	sb.WriteString("INSERT INTO " + quote(b.table) + " VALUES ")
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(row)
	}
	st, err := tx.Prepare(sb.String())
	if err != nil {
		return nil, fmt.Errorf("prepare %s x%d: %w", b.table, n, err)
	}
	b.stmts[n] = st
	return st, nil
}

// batch returns the staging buffer for table, or nil when the table is outside
// the plan. Hot loops append straight into b.rows: forwarding individual values
// through a variadic helper allocates a fresh []any for every row, which on the
// 300k-row tables means millions of short-lived objects.
func (l *loader) batch(table string) *tableBatch {
	if !l.p.wants(table) {
		return nil
	}
	b := l.batches[table]
	if b == nil {
		n := len(CatalogByName[table].Cols)
		max := batchMaxParams / n
		if max < 1 {
			max = 1
		}
		b = &tableBatch{table: table, width: n, max: max, stmts: map[int]*sql.Stmt{}}
		l.batches[table] = b
	}
	return b
}

// done records one staged row and flushes the batch when it is full.
func (l *loader) done(b *tableBatch) error {
	b.count++
	if b.count >= b.max {
		return l.writeBatch(b)
	}
	return nil
}

// ins inserts one row into a catalog table. Column order must match the
// catalog. Tables outside the plan are skipped without touching the database.
func (l *loader) ins(table string, vals ...any) error {
	b := l.batch(table)
	if b == nil {
		return nil
	}
	if len(vals) != b.width {
		return fmt.Errorf("store: %s expects %d columns, got %d", table, b.width, len(vals))
	}
	b.rows = append(b.rows, vals...)
	return l.done(b)
}

func (l *loader) writeBatch(b *tableBatch) error {
	if b.count == 0 {
		return nil
	}
	st, err := b.stmt(l.tx, b.count)
	if err != nil {
		return err
	}
	if _, err := st.Exec(b.rows...); err != nil {
		return fmt.Errorf("insert %s: %w", b.table, err)
	}
	b.rows = b.rows[:0]
	b.count = 0
	return nil
}

// flush writes the partial batch of every table.
func (l *loader) flush() error {
	for _, b := range l.batches {
		if err := l.writeBatch(b); err != nil {
			return err
		}
	}
	return nil
}

// load writes the rows selected by p and leaves the catalog ready to query.
// It runs once at open and again from EnsureTables for a lazy store, so every
// step must be safe to repeat for a table that is not built yet: `ins` drops
// rows for tables outside the plan, which is what makes a second call write only
// the delta.
func (s *Store) load(profile Profile, p plan) error {
	debug := os.Getenv("DEXSQL_DEBUG_LOAD") != ""
	built := s.nextBuilt(p)

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	l := &loader{tx: tx, s: s, profile: profile, p: p, batches: map[string]*tableBatch{}}
	tLoad := time.Now()
	if err := l.run(); err != nil {
		tx.Rollback()
		return err
	}
	if err := l.flush(); err != nil {
		tx.Rollback()
		return err
	}
	if err := s.writeMeta(tx, built); err != nil {
		tx.Rollback()
		return err
	}
	if debug {
		fmt.Fprintf(os.Stderr, "load rows: %v (%d tables)\n", time.Since(tLoad), len(built))
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	tIdx := time.Now()
	for _, idx := range indexDDL {
		if !p.wants(idx.table) {
			continue
		}
		if _, err := s.DB.Exec(idx.sql); err != nil {
			return fmt.Errorf("index: %w", err)
		}
	}
	if debug {
		fmt.Fprintf(os.Stderr, "indexes: %v\n", time.Since(tIdx))
	}
	s.built = built
	return nil
}

// indexDef ties an index to the table it accelerates, so a restricted build
// creates only the indexes it can use.
type indexDef struct {
	table string
	sql   string
}

// Only indexes that serve an equality lookup are kept. A B-tree on descriptor,
// strings.value or method_strings.value is never used by the LIKE '%x%' queries
// people actually run, and those three indexes alone cost ~18 MB and ~1.2 s on
// a mid-sized APK. The remaining ones answer the join and call-site questions.
var indexDDL = []indexDef{
	{"methods", `CREATE INDEX IF NOT EXISTS idx_methods_class ON methods(class_descriptor)`},
	{"method_calls", `CREATE INDEX IF NOT EXISTS idx_method_calls_callee ON method_calls(callee)`},
	{"method_calls", `CREATE INDEX IF NOT EXISTS idx_method_calls_method ON method_calls(method)`},
	{"method_refs", `CREATE INDEX IF NOT EXISTS idx_method_refs_kind ON method_refs(index_kind, "index")`},
	{"class_methods", `CREATE INDEX IF NOT EXISTS idx_class_methods_method ON class_methods(method_idx)`},
	{"types", `CREATE INDEX IF NOT EXISTS idx_types_class ON types(file_id, descriptor)`},
}

// step is one loader phase and the tables it fills. A phase is skipped when the
// plan wants none of them, which is what keeps an annotations-free query from
// walking the annotation tree at all.
type step struct {
	name   string
	tables []string
	fn     func(*loader) error
}

var loadSteps = []step{
	{"dexFiles", []string{"dex_files"}, (*loader).dexFiles},
	{"strings", []string{"strings"}, (*loader).strings},
	{"types", []string{"type_ids", "types"}, (*loader).types},
	{"protos", []string{"proto_ids", "protos", "proto_params"}, (*loader).protos},
	{"fields", []string{"field_ids", "fields"}, (*loader).fields},
	{"methods", []string{"method_ids", "methods", "method_opcodes"}, (*loader).methods},
	{"classes", []string{"class_defs", "classes", "class_interfaces"}, (*loader).classes},
	{"classMembers", []string{
		"class_data_items", "class_data_fields", "class_fields", "class_data_methods", "class_methods",
	}, (*loader).classMembers},
	{"codeItems", []string{"code_items", "try_items", "catch_handlers"}, (*loader).codeItems},
	{"bytecodeRefs", []string{
		"method_refs", "method_strings", "method_calls", "method_field_accesses",
		"method_used_types", "method_numbers", "method_using_numbers",
	}, (*loader).bytecodeRefs},
	{"annotations", []string{
		"annotations", "annotation_elements", "annotation_directories", "annotation_directory_entries",
		"annotation_sets", "annotation_set_entries", "annotation_set_ref_lists", "annotation_set_refs",
		"annotation_elements_resolved", "annotation_values", "encoded_arrays", "encoded_array_values",
		"encoded_array_owners", "class_static_values",
	}, (*loader).annotations},
	{"debugInfo", []string{"debug_info", "debug_parameters", "debug_positions", "debug_locals"}, (*loader).debugInfo},
	{"hiddenApi", []string{"hiddenapi_flags"}, (*loader).hiddenApi},
	{"layout", []string{
		"map_items", "type_lists", "type_list_entries", "id_items",
		"method_handles", "call_sites", "call_site_args",
	}, (*loader).layout},
}

func (l *loader) run() error {
	debug := os.Getenv("DEXSQL_DEBUG_LOAD") != ""
	for _, st := range loadSteps {
		run := false
		for _, t := range st.tables {
			if l.p.wants(t) {
				run = true
				break
			}
		}
		if !run {
			continue
		}
		t := time.Now()
		if err := st.fn(l); err != nil {
			return err
		}
		if debug {
			fmt.Fprintf(os.Stderr, "  %-14s %v\n", st.name, time.Since(t))
		}
	}
	return nil
}

// nextBuilt is the table set a catalog has after a load with plan p.
func (s *Store) nextBuilt(p plan) map[string]bool {
	out := make(map[string]bool, len(s.built)+len(p.names))
	for k := range s.built {
		out[k] = true
	}
	if p.need == nil {
		for _, n := range DefaultTables(s.Profile) {
			out[n] = true
		}
		return out
	}
	for _, n := range p.names {
		out[n] = true
	}
	return out
}

func (s *Store) writeMeta(tx *sql.Tx, built map[string]bool) error {
	names := make([]string, 0, len(built))
	for n := range built {
		names = append(names, n)
	}
	sort.Strings(names)
	_, err := tx.Exec(`INSERT OR REPLACE INTO _meta(key, value) VALUES('tables', ?)`, strings.Join(names, ","))
	return err
}

func (s *Store) readMeta(fallback []string) error {
	var v string
	err := s.DB.QueryRow(`SELECT value FROM _meta WHERE key='tables'`).Scan(&v)
	if err == sql.ErrNoRows {
		for _, n := range fallback {
			s.built[n] = true
		}
		return nil
	}
	if err != nil {
		return err
	}
	for _, n := range strings.Split(v, ",") {
		if n != "" {
			s.built[n] = true
		}
	}
	return nil
}

// Tables reports which relations this catalog can answer from. Tables left out
// exist (so column metadata and empty-result queries still work) but hold no
// rows.
func (s *Store) Tables() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.built))
	for n := range s.built {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ReleaseModel drops the parsed DEX model. A one-shot SQL query never needs it
// again — only tree, smali and search do — and a large APK holds a few hundred
// megabytes there.
func (s *Store) ReleaseModel() {
	s.Container = nil
}

// ensureLocked materialises whatever the statement reads that this catalog does
// not have yet. A lazy store can add tables in place; a fixed catalog cannot,
// and saying so is better than returning zero rows for a table nobody built.
func (s *Store) ensureLocked(q string) error {
	tables, ok := TablesForSQL(q)
	if !ok || len(tables) == 0 {
		return nil
	}
	var missing []string
	for _, t := range tables {
		if !s.built[t] {
			missing = append(missing, t)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if !s.lazy {
		return fmt.Errorf("relation %s was not materialised for this statement; rerun with --all-tables",
			strings.Join(missing, ", "))
	}
	if s.Container == nil {
		return fmt.Errorf("catalog lacks %s and the parsed model is gone", strings.Join(missing, ", "))
	}
	// query_only is per connection; this store owns its single connection.
	if _, err := s.DB.Exec("PRAGMA query_only=OFF"); err != nil {
		return err
	}
	defer s.DB.Exec("PRAGMA query_only=ON")
	return s.load(s.Profile, newPlan(missing, s.Profile))
}

// Query executes a single read-only statement and returns at most limit rows.
func (s *Store) Query(q string, limit int) (*Result, error) {
	start := time.Now()
	if err := Guard(q); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLocked(q); err != nil {
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
	res := &Result{Columns: cols, Rows: [][]any{}, RowCount: 0}
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
