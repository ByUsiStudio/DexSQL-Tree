package store

import (
	"sort"
	"strings"
)

// Materialising the whole catalog costs seconds and hundreds of megabytes,
// while a typical question touches one or two tables: "who calls X" needs
// method_calls alone, "methods-like" needs methods plus types. The plan below
// turns a SQL statement into the smallest set of base tables that can answer
// it, so a cold query pays for what it reads instead of for the whole DEX.
//
// The analysis is lexical, not a SQL parser: it finds identifiers outside
// comments, string literals and quoted identifiers, then keeps the ones that
// name a relation. Anything it cannot see (a view, a CTE, a table-valued
// function) is either expanded here or simply not materialised, and the
// statement then fails in SQLite with "no such table" — the same error a user
// would get from a hand-written database. There is no silent wrong answer,
// because Query verifies the plan against what the catalog actually built.

// viewDeps maps each convenience view to the base tables its DDL reads. The
// view definitions are static, so the dependency list is written down here
// instead of being re-parsed at runtime.
var viewDeps = map[string][]string{
	"v_methods":  {"methods", "types"},
	"v_fields":   {"fields", "types"},
	"v_overview": {"dex_files", "classes", "methods", "fields", "strings", "method_calls", "method_strings", "annotations"},
}

// derivedFast lists tables that a fast build leaves empty by default because
// they are supersets of other tables:
//
//	method_refs           every ref of method_calls + method_field_accesses +
//	                      method_used_types + method_strings + method_numbers
//	method_using_numbers  the same rows as method_numbers, with the caller
//	                      columns already present in that table
//	id_items              raw byte offsets, only useful for forensic queries
//
// A statement that names one of them explicitly still gets it: the point of
// on-demand materialisation is that the query decides, not the profile.
var derivedFast = map[string]bool{
	"method_refs":          true,
	"method_using_numbers": true,
	"id_items":             true,
}

// AllTables returns every catalog table in declaration order.
func AllTables() []string {
	out := make([]string, len(Catalog))
	for i, t := range Catalog {
		out[i] = t.Name
	}
	return out
}

// DefaultTables is what a build writes when the caller does not restrict it.
// The fast profile drops the derived tables above; annotation tables stay in
// the list because the profile already leaves them empty, and querying them
// must keep returning zero rows rather than an error.
func DefaultTables(p Profile) []string {
	out := make([]string, 0, len(Catalog))
	for _, t := range Catalog {
		if p != ProfileFull && derivedFast[t.Name] {
			continue
		}
		out = append(out, t.Name)
	}
	return out
}

// resolveTables expands views into base tables and drops names that are not
// relations (aliases, CTE names, column names). The result is sorted so a plan
// has a stable identity for cache keys.
func resolveTables(names []string) []string {
	set := make(map[string]bool, len(names))
	var add func(string)
	add = func(name string) {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || set[name] {
			return
		}
		if _, ok := CatalogByName[name]; ok {
			set[name] = true
			return
		}
		if deps, ok := viewDeps[name]; ok {
			for _, d := range deps {
				add(d)
			}
		}
	}
	for _, n := range names {
		add(n)
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// isIdentByte reports whether c can appear in a bare SQL identifier.
func isIdentByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c == '_' || c == '$'
}

// TablesForSQL returns the base tables a read-only statement reads, along with
// whether it named any relation at all.
//
//	SELECT * FROM methods WHERE name = 'x'   -> [methods]
//	SELECT * FROM v_methods                  -> [methods types]
//	SELECT count(*) FROM method_calls        -> [method_calls]
//	SELECT 1                                 -> []
//
// A false second result means "nothing matched"; callers should treat it as an
// empty plan and let SQLite report the real problem, because materialising all
// 56 tables to answer SELECT 1 is exactly the cost this function avoids.
func TablesForSQL(sql string) (tables []string, resolved bool) {
	seen := make(map[string]bool, 8)
	add := func(word string) {
		word = strings.ToLower(word)
		if _, ok := CatalogByName[word]; ok {
			seen[word] = true
			return
		}
		if _, ok := viewDeps[word]; ok {
			seen[word] = true
		}
	}
	for i, n := 0, len(sql); i < n; {
		c := sql[i]
		switch {
		case c == '-' && i+1 < n && sql[i+1] == '-':
			i += 2
			for i < n && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && sql[i+1] == '*':
			i += 2
			for i+1 < n && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			if i+1 < n {
				i += 2
			} else {
				i = n
			}
		case c == '\'':
			// String literal; doubled quotes are an escaped quote.
			i++
			for i < n {
				if sql[i] == '\'' {
					if i+1 < n && sql[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case c == '"' || c == '`':
			quote := c
			i++
			start := i
			for i < n && sql[i] != quote {
				i++
			}
			add(sql[start:i])
			if i < n {
				i++
			}
		case c == '[':
			i++
			start := i
			for i < n && sql[i] != ']' {
				i++
			}
			add(sql[start:i])
			if i < n {
				i++
			}
		case isIdentByte(c):
			start := i
			for i < n && isIdentByte(sql[i]) {
				i++
			}
			add(sql[start:i])
		default:
			i++
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return resolveTables(out), len(out) > 0
}

// plan is the resolved table set for one build.
type plan struct {
	// need is nil when every table in the profile is wanted, which lets the
	// loader skip the per-row membership test.
	need map[string]bool
	// names is the sorted list, nil for "all".
	names []string
}

func newPlan(tables []string, profile Profile) plan {
	if tables == nil {
		return plan{}
	}
	names := resolveTables(tables)
	need := make(map[string]bool, len(names))
	for _, n := range names {
		need[n] = true
	}
	return plan{need: need, names: names}
}

// wants reports whether a table should receive rows.
func (p plan) wants(table string) bool {
	return p.need == nil || p.need[table]
}

// missing lists the tables in the plan that a catalog has not built.
func (p plan) missing(built map[string]bool) []string {
	if p.need == nil {
		return nil
	}
	var out []string
	for _, n := range p.names {
		if !built[n] {
			out = append(out, n)
		}
	}
	return out
}

// subsetOfDefault reports whether a restricted plan only asks for tables that
// the profile's default build already writes. Such a plan can be served by an
// existing full catalog; a plan that needs a derived table cannot, because a
// fast catalog leaves that table empty and the query would silently lose rows.
func subsetOfDefault(names []string, profile Profile) bool {
	allowed := make(map[string]bool, len(Catalog))
	for _, t := range DefaultTables(profile) {
		allowed[t] = true
	}
	for _, n := range names {
		if !allowed[n] {
			return false
		}
	}
	return true
}
