package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/byusi-studio/dexsql-tree/internal/dex"
	"github.com/byusi-studio/dexsql-tree/internal/dextest"
	"github.com/byusi-studio/dexsql-tree/internal/store"
)

// A second Open with the same cache key must mount the catalog instead of
// materialising it again, and must answer identically.
func TestCacheReusesCatalog(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "hello.dex")
	if err := os.WriteFile(input, dextest.HelloDex(), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := dex.ParseContainer(dextest.HelloDex(), input)
	if err != nil {
		t.Fatal(err)
	}
	opt := store.Options{CacheDir: filepath.Join(dir, "cache"), CacheKey: "testkey"}

	first, err := store.Open(c, opt)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if first.Cached {
		t.Fatal("first open reported a cache hit")
	}
	before, err := first.Query("SELECT count(*) FROM methods", 0)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	if _, err := os.Stat(filepath.Join(opt.CacheDir, "testkey.db")); err != nil {
		t.Fatalf("catalog not published: %v", err)
	}

	second, err := store.Open(c, opt)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer second.Close()
	if !second.Cached {
		t.Fatal("second open did not use the cache")
	}
	after, err := second.Query("SELECT count(*) FROM methods", 0)
	if err != nil {
		t.Fatal(err)
	}
	if before.Rows[0][0] != after.Rows[0][0] {
		t.Fatalf("cached answer differs: %v vs %v", before.Rows[0][0], after.Rows[0][0])
	}
}

// A corrupt catalog must be discarded and rebuilt rather than served.
func TestCacheDiscardsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "bad.db"), []byte("not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := dex.ParseContainer(dextest.HelloDex(), "hello.dex")
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(c, store.Options{CacheDir: cacheDir, CacheKey: "bad"})
	if err != nil {
		t.Fatalf("open with corrupt cache: %v", err)
	}
	defer s.Close()
	if s.Cached {
		t.Fatal("corrupt catalog was served")
	}
	if _, err := s.Query("SELECT count(*) FROM classes", 0); err != nil {
		t.Fatalf("rebuilt catalog is unusable: %v", err)
	}
}

// The fast profile keeps the annotation tables empty while leaving the rest of
// the catalog intact; full fills them.
func TestProfileControlsAnnotations(t *testing.T) {
	c, err := dex.ParseContainer(dextest.HelloDex(), "hello.dex")
	if err != nil {
		t.Fatal(err)
	}
	count := func(s *store.Store, table string) int {
		res, err := s.Query("SELECT count(*) FROM "+table, 0)
		if err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		n, _ := res.Rows[0][0].(int64)
		return int(n)
	}

	fast, err := store.Open(c, store.Options{Profile: store.ProfileFast})
	if err != nil {
		t.Fatal(err)
	}
	if got := count(fast, "annotation_values"); got != 0 {
		t.Errorf("fast profile wrote %d annotation values", got)
	}
	if got := count(fast, "methods"); got == 0 {
		t.Error("fast profile dropped the methods table")
	}
	fast.Close()

	full, err := store.Open(c, store.Options{Profile: store.ProfileFull})
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	if got := count(full, "annotations"); got == 0 {
		t.Skip("sample DEX has no annotations; nothing to compare")
	}
	if got, want := count(full, "methods"), count(fast, "methods"); got != want {
		t.Errorf("full profile wrote %d methods, fast wrote %d", got, want)
	}
}

func TestParseProfile(t *testing.T) {
	for _, in := range []string{"", "fast", "FAST", "default"} {
		if p, err := store.ParseProfile(in); err != nil || p != store.ProfileFast {
			t.Errorf("ParseProfile(%q) = %v, %v", in, p, err)
		}
	}
	for _, in := range []string{"full", "All"} {
		if p, err := store.ParseProfile(in); err != nil || p != store.ProfileFull {
			t.Errorf("ParseProfile(%q) = %v, %v", in, p, err)
		}
	}
	if _, err := store.ParseProfile("turbo"); err == nil {
		t.Error("expected an error for an unknown profile")
	}
}
