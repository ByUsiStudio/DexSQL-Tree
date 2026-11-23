package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKeyIsOrderInsensitiveAndTracksMtime(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.apk")
	b := filepath.Join(dir, "b.apk")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("dex"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	k1 := Key([]string{a, b}, "fast", "0.3.0", false)
	k2 := Key([]string{b, a}, "fast", "0.3.0", false)
	if k1 != k2 {
		t.Errorf("key depends on argument order: %s vs %s", k1, k2)
	}
	if k3 := Key([]string{a}, "fast", "0.3.0", false); k3 == k1 {
		t.Error("adding an input must change the key")
	}
	if k4 := Key([]string{a, b}, "full", "0.3.0", false); k4 == k1 {
		t.Error("changing the profile must change the key")
	}
	if k5 := Key([]string{a, b}, "fast", "0.3.0", true); k5 == k1 {
		t.Error("changing the debug flag must change the key")
	}

	// A changed mtime invalidates the catalog even when the bytes are identical.
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(a, future, future); err != nil {
		t.Fatal(err)
	}
	if k6 := Key([]string{a, b}, "fast", "0.3.0", false); k6 == k1 {
		t.Error("touching an input must change the key")
	}
}

func TestListAndClear(t *testing.T) {
	dir := t.TempDir()
	if ents, err := List(filepath.Join(dir, "missing")); err != nil || len(ents) != 0 {
		t.Fatalf("missing dir: %v %v", ents, err)
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"aaa.db", "bbb.db"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Files that are not catalogs must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ents, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 2 {
		t.Fatalf("want 2 entries, got %d", len(ents))
	}
	n, bytes, err := Clear(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || bytes != 2 {
		t.Errorf("cleared %d files / %d bytes", n, bytes)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Error("clear removed an unrelated file")
	}
}

func TestStatRejectsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(dir, "empty"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Stat(dir, "empty"); ok {
		t.Error("an empty file must not count as a catalog")
	}
}
