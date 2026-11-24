// Package cache stores materialised catalogs between runs.
//
// Building the SQLite catalog for a large APK dominates every command: the same
// 49 MB package takes tens of seconds to open, and analysts open it repeatedly.
// The parse is unavoidable (the call tree and smali need the in-memory model),
// but the catalog is a pure function of the input, so it can be reused.
//
// A cache key is derived from the absolute path, size and modification time of
// every input plus the engine version, materialisation profile and debug flag.
// Editing an input changes its key, so stale catalogs are never served.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry is one cached catalog file.
type Entry struct {
	Key     string    `json:"key"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// Dir resolves the cache directory: DEXSQL_TREE_CACHE wins, then the platform
// user cache directory.
func Dir() string {
	if v := strings.TrimSpace(os.Getenv("DEXSQL_TREE_CACHE")); v != "" {
		return v
	}
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "dexsql-tree")
}

// Key fingerprints the inputs and the materialisation settings. Paths are sorted
// so "--input a --input b" and "--input b --input a" share one catalog.
func Key(paths []string, profile, version string, debug bool) string {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	h := sha256.New()
	fmt.Fprintf(h, "version=%s\nprofile=%s\ndebug=%v\n", version, profile, debug)
	for _, p := range sorted {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		var size int64
		var mtime int64
		if fi, err := os.Stat(p); err == nil {
			size = fi.Size()
			mtime = fi.ModTime().UnixNano()
		}
		fmt.Fprintf(h, "%s|%d|%d\n", abs, size, mtime)
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// Path is where a catalog for key lives.
func Path(dir, key string) string {
	return filepath.Join(dir, key+".db")
}

// Subkey folds the table set of an on-demand build into a catalog key.
//
// A catalog built for two tables cannot answer a statement that reads five, so
// the set is part of the cache identity. Sorting keeps "--sql a" and "--sql b"
// that read the same tables on one catalog. An empty set returns the base key,
// because a statement that reads no relation has nothing to cache.
func Subkey(base string, tables []string) string {
	if len(tables) == 0 {
		return base
	}
	sorted := append([]string(nil), tables...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, ",")))
	// Keep a prefix of the base key so `cache list` groups one input's
	// catalogs together and `cache info` can find them again.
	prefix := base
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	return prefix + "-" + hex.EncodeToString(sum[:])[:8]
}

// ForInput returns the catalogs belonging to one input: the full catalog plus
// every on-demand subset built for it.
func ForInput(dir, base string) ([]Entry, error) {
	ents, err := List(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, 2)
	for _, e := range ents {
		if e.Key == base || strings.HasPrefix(e.Key, base[:min(len(base), 12)]+"-") {
			out = append(out, e)
		}
	}
	return out, nil
}

// Stat reports whether a catalog exists for key.
func Stat(dir, key string) (Entry, bool) {
	p := Path(dir, key)
	fi, err := os.Stat(p)
	if err != nil || fi.Size() == 0 {
		return Entry{}, false
	}
	return Entry{Key: key, Path: p, Size: fi.Size(), ModTime: fi.ModTime()}, true
}

// List returns every cached catalog, newest first.
func List(dir string) ([]Entry, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Entry, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Entry{
			Key:     strings.TrimSuffix(e.Name(), ".db"),
			Path:    filepath.Join(dir, e.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

// Clear removes every catalog and reports how many files and bytes went away.
func Clear(dir string) (int, int64, error) {
	ents, err := List(dir)
	if err != nil {
		return 0, 0, err
	}
	n := 0
	var bytes int64
	for _, e := range ents {
		if err := os.Remove(e.Path); err == nil {
			n++
			bytes += e.Size
		}
	}
	return n, bytes, nil
}

// Ensure creates the cache directory.
func Ensure(dir string) error { return os.MkdirAll(dir, 0o755) }

// CleanStale removes abandoned temporary catalogs. A process killed between
// writing and publishing leaves a <key>.db.tmp<pid> file behind; anything older
// than olderThan is safe to drop because publishing takes milliseconds.
func CleanStale(dir string, olderThan time.Duration) int {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-olderThan)
	n := 0
	for _, e := range ents {
		if e.IsDir() || !strings.Contains(e.Name(), ".db.tmp") {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(dir, e.Name())) == nil {
			n++
		}
	}
	return n
}
