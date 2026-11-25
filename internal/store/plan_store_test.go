package store_test

import (
	"strings"
	"testing"

	"github.com/byusi-studio/dexsql-tree/internal/cache"
	"github.com/byusi-studio/dexsql-tree/internal/dex"
	"github.com/byusi-studio/dexsql-tree/internal/dextest"
	"github.com/byusi-studio/dexsql-tree/internal/store"
)

func openWith(t *testing.T, opt store.Options) (*store.Store, *dex.Container) {
	t.Helper()
	c, err := dex.ParseContainer(dextest.HelloDex(), "hello.dex")
	if err != nil {
		t.Fatalf("parse container: %v", err)
	}
	s, err := store.Open(c, opt)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, c
}

func hasTable(s *store.Store, name string) bool {
	for _, n := range s.Tables() {
		if n == name {
			return true
		}
	}
	return false
}

// A catalog built for two tables must answer their queries and refuse a table
// nobody built, instead of returning zero rows for it.
func TestPartialMaterialisation(t *testing.T) {
	s, _ := openWith(t, store.Options{Tables: []string{"methods", "types"}})
	if got := queryStrings(t, s, "SELECT count(*) FROM methods"); got[0] != "2" {
		t.Fatalf("methods count = %v", got)
	}
	if hasTable(s, "_call_sites") {
		t.Fatalf("_call_sites should not be built: %v", s.Tables())
	}
	_, err := s.Query("SELECT count(*) FROM method_calls", 0)
	if err == nil || !strings.Contains(err.Error(), "not materialised") {
		t.Fatalf("expected a not-materialised error, got %v", err)
	}
}

// An empty plan means "no table data", which is the right answer for a
// statement that reads none, and it must not be silently upgraded to a build.
func TestEmptyPlan(t *testing.T) {
	s, _ := openWith(t, store.Options{Tables: []string{}})
	if len(s.Tables()) != 0 {
		t.Fatalf("built tables = %v", s.Tables())
	}
	res, err := s.Query("SELECT 1 AS x", 0)
	if err != nil {
		t.Fatalf("select 1: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("rows = %d", len(res.Rows))
	}
}

// The fast profile leaves the derived tables out of a default build, but a
// statement that names one still gets it: the query decides, not the profile.
func TestDerivedTableOnDemand(t *testing.T) {
	s, _ := openWith(t, store.Options{})
	if hasTable(s, "method_refs") {
		t.Fatalf("default fast build should skip method_refs")
	}
	s2, _ := openWith(t, store.Options{Tables: []string{"method_refs"}})
	if got := queryStrings(t, s2, "SELECT count(*) FROM method_refs"); got[0] == "0" {
		t.Fatalf("method_refs should have rows when requested explicitly")
	}
}

// A long-lived server does not know its statements at open time, so tables are
// added as queries arrive and reused afterwards.
func TestLazyStoreAddsTables(t *testing.T) {
	s, _ := openWith(t, store.Options{Lazy: true})
	if len(s.Tables()) != 0 {
		t.Fatalf("lazy store built tables at open: %v", s.Tables())
	}
	if got := queryStrings(t, s, "SELECT count(*) FROM methods"); got[0] != "2" {
		t.Fatalf("methods count = %v", got)
	}
	if !hasTable(s, "methods") || hasTable(s, "_call_sites") {
		t.Fatalf("after first query built = %v", s.Tables())
	}
	if got := queryStrings(t, s, "SELECT count(*) FROM method_calls"); got[0] != "1" {
		t.Fatalf("method_calls count = %v", got)
	}
	if !hasTable(s, "_call_sites") {
		t.Fatalf("_call_sites not added: %v", s.Tables())
	}
	// A second query over the same tables must not duplicate rows.
	if got := queryStrings(t, s, "SELECT count(*) FROM methods"); got[0] != "2" {
		t.Fatalf("methods count after re-query = %v", got)
	}
}

// One input gets one catalog file. A later query that reads a table the file
// lacks must grow that file, not start a second copy: keying by table set left
// a single 11 MB APK at 490 MB of cache.
func TestCacheGrowsInPlace(t *testing.T) {
	dir := t.TempDir()
	openWith(t, store.Options{CacheDir: dir, CacheKey: "c", Tables: []string{"methods"}})

	s, _ := openWith(t, store.Options{CacheDir: dir, CacheKey: "c",
		Tables: []string{"methods", "_call_sites"}})
	if s.Cached {
		t.Fatalf("a grown catalog reports the tables it built, not a plain hit")
	}
	ents, err := cache.List(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ents) != 1 {
		t.Fatalf("cache should hold one catalog, got %d: %v", len(ents), ents)
	}
	if got := queryStrings(t, s, "SELECT count(*) FROM method_calls"); got[0] != "1" {
		t.Fatalf("method_calls rows after grow = %v", got)
	}
	// The tables the first query paid for are still there.
	if got := queryStrings(t, s, "SELECT count(*) FROM methods"); got[0] != "2" {
		t.Fatalf("methods rows after grow = %v", got)
	}

	s2, _ := openWith(t, store.Options{CacheDir: dir, CacheKey: "c",
		Tables: []string{"methods", "_call_sites"}})
	if !s2.Cached {
		t.Fatalf("the grown catalog should answer the same plan from disk")
	}
	if got := queryStrings(t, s2, "SELECT count(*) FROM method_calls"); got[0] != "1" {
		t.Fatalf("method_calls rows from disk = %v", got)
	}
}

// A lazy fast store must not mount a fast catalog: the file cannot grow, and a
// later query may legitimately name a derived table the fast build skipped.
func TestLazySkipsIncompleteCache(t *testing.T) {
	dir := t.TempDir()
	openWith(t, store.Options{CacheDir: dir, CacheKey: "fast"})
	s, _ := openWith(t, store.Options{CacheDir: dir, CacheKey: "fast", Lazy: true})
	if s.Cached {
		t.Fatalf("lazy fast store mounted a catalog missing the derived tables")
	}
	if got := queryStrings(t, s, "SELECT count(*) FROM method_refs"); got[0] == "0" {
		t.Fatalf("method_refs should be built on demand, got %v", got)
	}
}

// A full catalog covers every table, so a lazy store can mount it read-only.
func TestLazyReusesFullCache(t *testing.T) {
	dir := t.TempDir()
	openWith(t, store.Options{CacheDir: dir, CacheKey: "full", Profile: store.ProfileFull})
	s, _ := openWith(t, store.Options{CacheDir: dir, CacheKey: "full", Profile: store.ProfileFull, Lazy: true})
	if !s.Cached {
		t.Fatalf("lazy full store should mount the complete catalog")
	}
	if got := queryStrings(t, s, "SELECT count(*) FROM method_refs"); got[0] == "0" {
		t.Fatalf("full catalog should already hold method_refs")
	}
}
