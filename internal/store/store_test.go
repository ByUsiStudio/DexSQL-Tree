package store_test

import (
	"fmt"
	"testing"

	"github.com/byusi-studio/dexsql-tree/internal/dex"
	"github.com/byusi-studio/dexsql-tree/internal/dextest"
	"github.com/byusi-studio/dexsql-tree/internal/store"
)

func openHello(t *testing.T) *store.Store {
	t.Helper()
	c, err := dex.ParseContainer(dextest.HelloDex(), "hello.dex")
	if err != nil {
		t.Fatalf("parse container: %v", err)
	}
	s, err := store.Open(c)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func queryStrings(t *testing.T, s *store.Store, q string) []string {
	t.Helper()
	res, err := s.Query(q, 0)
	if err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	if len(res.Rows) == 0 {
		t.Fatalf("query %q returned no rows", q)
	}
	out := make([]string, len(res.Rows))
	for i, r := range res.Rows {
		out[i] = fmt.Sprint(r[0])
	}
	return out
}

func TestEntityQueries(t *testing.T) {
	s := openHello(t)
	if got := queryStrings(t, s, "SELECT descriptor FROM methods WHERE name = 'main'"); len(got) != 1 || got[0] != "Ldemo/Hello;->main([Ljava/lang/String;)V" {
		t.Fatalf("methods = %v", got)
	}
	if got := queryStrings(t, s, "SELECT descriptor FROM classes"); len(got) != 1 || got[0] != "Ldemo/Hello;" {
		t.Fatalf("classes = %v", got)
	}
	var n int
	res, err := s.Query("SELECT count(*) FROM types WHERE descriptor = '[I'", 0)
	if err != nil {
		t.Fatalf("array type query: %v", err)
	}
	n = int(res.Rows[0][0].(int64))
	if n != 0 {
		t.Fatalf("unexpected array types: %d", n)
	}
	if got := queryStrings(t, s, "SELECT source_file FROM classes WHERE descriptor = 'Ldemo/Hello;'"); got[0] != "Hello.java" {
		t.Fatalf("source_file = %v", got)
	}
}

func TestReferenceViews(t *testing.T) {
	s := openHello(t)
	if got := queryStrings(t, s, "SELECT value FROM method_strings"); len(got) != 1 || got[0] != "Hello from DexSQL-Tree" {
		t.Fatalf("method_strings = %v", got)
	}
	if got := queryStrings(t, s, "SELECT callee FROM method_calls"); len(got) != 1 || got[0] != "Ljava/io/PrintStream;->println(Ljava/lang/String;)V" {
		t.Fatalf("method_calls = %v", got)
	}
	if got := queryStrings(t, s, "SELECT field FROM method_field_accesses WHERE using_type = 'get'"); len(got) != 1 || got[0] != "Ljava/lang/System;->out:Ljava/io/PrintStream;" {
		t.Fatalf("field accesses = %v", got)
	}
	res2, err := s.Query("SELECT count(*) FROM method_used_types", 0)
	if err != nil {
		t.Fatalf("used types: %v", err)
	}
	if res2.Rows[0][0].(int64) != 0 {
		t.Fatalf("used types should be empty")
	}
	if got := queryStrings(t, s, "SELECT insns_size FROM code_items"); len(got) != 1 {
		t.Fatalf("code_items = %v", got)
	}
	if got := queryStrings(t, s, "SELECT opcode_count FROM method_opcodes WHERE method_idx = 0"); len(got) != 1 || got[0] != "4" {
		t.Fatalf("method_opcodes = %v", got)
	}
}

func TestJoinAcrossTables(t *testing.T) {
	s := openHello(t)
	q := `
		SELECT c.descriptor, m.method_name, mm.param_count
		FROM classes c
		JOIN class_methods m ON m.file_id = c.file_id AND m.class_idx = c.class_idx
		JOIN methods mm ON mm.file_id = m.file_id AND mm.method_idx = m.method_idx
		WHERE m.method_name = 'main'`
	res, err := s.Query(q, 10)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("join rows = %d", len(res.Rows))
	}
}

func TestReadOnlyGuard(t *testing.T) {
	s := openHello(t)
	for _, q := range []string{"DELETE FROM methods", "PRAGMA table_info(methods)", "SELECT 1; DROP TABLE methods"} {
		if _, err := s.Query(q, 10); err == nil {
			t.Errorf("query %q should be rejected", q)
		}
	}
	if _, err := s.Query("SELECT count(*) FROM methods", 0); err != nil {
		t.Fatalf("select rejected: %v", err)
	}
}

func TestLimit(t *testing.T) {
	s := openHello(t)
	res, err := s.Query("SELECT value FROM strings", 2)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res.Rows) != 2 || !res.Truncated {
		t.Fatalf("rows=%d truncated=%v", len(res.Rows), res.Truncated)
	}
}

func TestCatalogComplete(t *testing.T) {
	s := openHello(t)
	res, err := s.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name", 0)
	if err != nil {
		t.Fatalf("sqlite_master: %v", err)
	}
	have := map[string]bool{}
	for _, r := range res.Rows {
		have[r[0].(string)] = true
	}
	for _, tb := range store.Catalog {
		if !have[tb.Name] {
			t.Errorf("catalog table %s missing", tb.Name)
		}
	}
}
