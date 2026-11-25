package store_test

import (
	"testing"

	"github.com/byusi-studio/dexsql-tree/internal/store"
)

func TestTablesForSQL(t *testing.T) {
	cases := []struct {
		sql      string
		want     []string
		resolved bool
	}{
		{"SELECT * FROM methods", []string{"methods"}, true},
		// method_calls is a view over the index-only table plus methods.
		{"select count(*) from METHOD_CALLS", []string{"_call_sites", "methods"}, true},
		{"SELECT m.name FROM methods m JOIN types t ON t.descriptor = m.class_descriptor",
			[]string{"methods", "types"}, true},
		// A view expands to the base tables its DDL reads.
		{"SELECT * FROM v_methods", []string{"methods", "types"}, true},
		{"SELECT * FROM v_overview", []string{
			"_call_sites", "_string_refs", "annotations", "classes", "dex_files",
			"fields", "methods", "strings"}, true},
		// Identifiers in comments and string literals are not relations.
		{"SELECT 'methods' AS x", nil, false},
		{"SELECT 1 -- from method_calls", nil, false},
		{"/* methods */ SELECT 1", nil, false},
		{"SELECT * FROM classes -- join method_calls", []string{"classes"}, true},
		// Quoted identifiers still count; unknown ones are ignored.
		{`SELECT * FROM "methods"`, []string{"methods"}, true},
		{"SELECT * FROM [methods]", []string{"methods"}, true},
		{"SELECT * FROM `methods`", []string{"methods"}, true},
		{"SELECT * FROM no_such_table", nil, false},
		// A CTE name that happens to match a table is a false positive by
		// design: the extra table costs a little time, never correctness.
		{"WITH x AS (SELECT 1) SELECT * FROM x", nil, false},
	}
	for _, c := range cases {
		got, resolved := store.TablesForSQL(c.sql)
		if resolved != c.resolved {
			t.Errorf("%q: resolved = %v, want %v", c.sql, resolved, c.resolved)
		}
		if len(got) != len(c.want) {
			t.Errorf("%q: tables = %v, want %v", c.sql, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: tables = %v, want %v", c.sql, got, c.want)
				break
			}
		}
	}
}

func TestDefaultTablesByProfile(t *testing.T) {
	fast := map[string]bool{}
	for _, name := range store.DefaultTables(store.ProfileFast) {
		fast[name] = true
	}
	for _, derived := range []string{"method_refs", "id_items"} {
		if fast[derived] {
			t.Errorf("fast profile should skip derived table %s", derived)
		}
	}
	// method_using_numbers is a view now: naming it materialises method_numbers,
	// so the default build no longer has to write the same rows twice.
	if fast["method_using_numbers"] {
		t.Errorf("method_using_numbers is not a physical table")
	}
	if !fast["_call_sites"] {
		t.Errorf("the index-only call table must be part of a default build")
	}
	if !fast["methods"] || !fast["annotations"] {
		t.Errorf("fast profile lost a base table: %v", store.DefaultTables(store.ProfileFast))
	}
	if len(store.DefaultTables(store.ProfileFull)) != len(store.Catalog) {
		t.Errorf("full profile should cover the whole catalog")
	}
}
