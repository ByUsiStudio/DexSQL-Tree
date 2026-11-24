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
		{"select count(*) from METHOD_CALLS", []string{"method_calls"}, true},
		{"SELECT m.name FROM methods m JOIN types t ON t.descriptor = m.class_descriptor",
			[]string{"methods", "types"}, true},
		// A view expands to the base tables its DDL reads.
		{"SELECT * FROM v_methods", []string{"methods", "types"}, true},
		{"SELECT * FROM v_overview", []string{
			"annotations", "classes", "dex_files", "fields", "method_calls",
			"method_strings", "methods", "strings"}, true},
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
	for _, derived := range []string{"method_refs", "method_using_numbers", "id_items"} {
		if fast[derived] {
			t.Errorf("fast profile should skip derived table %s", derived)
		}
	}
	if !fast["methods"] || !fast["annotations"] {
		t.Errorf("fast profile lost a base table: %v", store.DefaultTables(store.ProfileFast))
	}
	if len(store.DefaultTables(store.ProfileFull)) != len(store.Catalog) {
		t.Errorf("full profile should cover the whole catalog")
	}
}
