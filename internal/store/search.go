package store

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/byusi-studio/dexsql-tree/internal/dex"
)

// SearchKinds lists what Search can look at.
var SearchKinds = []string{"class", "method", "field", "string"}

// hit is one ranked search result.
type hit struct {
	kind, name, owner, detail string
	ownerMissing              bool
}

// Search finds classes, methods, fields and string constants by keyword, with
// exact and prefix matches ranked first.
//
// It scans the already-parsed model instead of running SQL: a LIKE '%x%' query
// would have to materialise the whole catalog first (17 s and 1.3 GB on an
// 11 MB APK), while the model is already in memory and the scan itself takes a
// few milliseconds. The result shape is the same either way, so the CLI, the
// HTTP API and MCP keep one contract.
func (s *Store) Search(keyword string, kinds []string, limit int) (*Result, error) {
	return Search(s.Container, keyword, kinds, limit)
}

// Search scans a container directly, which is what the CLI uses: no SQLite
// catalog is built, so searching an 11 MB APK takes the parse time only.
func Search(c *dex.Container, keyword string, kinds []string, limit int) (*Result, error) {
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return nil, fmt.Errorf("empty search keyword")
	}
	if len(kinds) == 0 {
		kinds = SearchKinds
	}
	if limit <= 0 {
		limit = 100
	}
	want := map[string]bool{}
	for _, k := range kinds {
		switch k {
		case "class", "method", "field", "string":
			want[k] = true
		default:
			return nil, fmt.Errorf("unknown search kind %q (use class, method, field or string)", k)
		}
	}
	start := time.Now()
	needle := strings.ToLower(kw)
	var hits []hit
	match := func(name string) bool {
		return strings.Contains(strings.ToLower(name), needle)
	}
	for _, f := range c.Files {
		if want["class"] {
			for _, cd := range f.Classes {
				if !match(cd.Descriptor) {
					continue
				}
				h := hit{kind: "class", name: cd.Descriptor, detail: cd.SourceFile}
				if cd.HasSuperclass {
					h.owner = cd.SuperDescriptor
				} else {
					h.ownerMissing = true
				}
				hits = append(hits, h)
			}
		}
		if want["method"] {
			kindOf := methodKinds(f)
			for i := range f.Methods {
				m := &f.Methods[i]
				if !match(m.Descriptor) {
					continue
				}
				hits = append(hits, hit{kind: "method", name: m.Descriptor, owner: m.ClassDesc, detail: kindOf[uint32(i)]})
			}
		}
		if want["field"] {
			for i := range f.Fields {
				fd := &f.Fields[i]
				desc := fd.Descriptor()
				if !match(desc) {
					continue
				}
				hits = append(hits, hit{kind: "field", name: desc, owner: fd.ClassDesc, detail: fd.TypeDesc})
			}
		}
		if want["string"] {
			for _, v := range f.Strings {
				if match(v) {
					hits = append(hits, hit{kind: "string", name: v, ownerMissing: true})
				}
			}
		}
	}
	rank := func(h hit) int {
		name := strings.ToLower(h.name)
		score := 2
		switch {
		case name == needle:
			score = 0
		case strings.HasPrefix(name, needle):
			score = 1
		}
		if h.kind == "string" {
			score++ // structural hits describe code, strings are usually noise
		}
		return score
	}
	kindRank := map[string]int{"class": 0, "method": 1, "field": 2, "string": 3}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra < rb
		}
		if ka, kb := kindRank[a.kind], kindRank[b.kind]; ka != kb {
			return ka < kb
		}
		if len(a.name) != len(b.name) {
			return len(a.name) < len(b.name)
		}
		return a.name < b.name
	})
	// Multi-dex inputs can repeat a descriptor; keep the first occurrence.
	seen := make(map[string]bool, len(hits))
	res := &Result{Columns: []string{"kind", "name", "owner", "detail"}}
	truncated := false
	for _, h := range hits {
		key := h.kind + "\x00" + h.name
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(res.Rows) >= limit {
			truncated = true
			break
		}
		var owner any
		if !h.ownerMissing {
			owner = h.owner
		}
		res.Rows = append(res.Rows, []any{h.kind, h.name, owner, h.detail})
	}
	res.RowCount = len(res.Rows)
	res.Truncated = truncated
	res.ElapsedMS = time.Since(start).Milliseconds()
	return res, nil
}

// methodKinds maps method index to "direct"/"virtual" for declared methods; a
// reference-only method has no entry and therefore no kind.
func methodKinds(f *dex.File) map[uint32]string {
	out := make(map[uint32]string, len(f.Methods))
	for _, cd := range f.Classes {
		if cd.ClassData == nil {
			continue
		}
		for _, m := range cd.ClassData.DirectMethods {
			out[m.MethodIdx] = "direct"
		}
		for _, m := range cd.ClassData.VirtualMethods {
			out[m.MethodIdx] = "virtual"
		}
	}
	return out
}
