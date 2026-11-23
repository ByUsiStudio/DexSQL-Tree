package store_test

import (
	"strings"
	"testing"

	"github.com/byusi-studio/dexsql-tree/internal/store"
)

func TestViewsExistAndAnswer(t *testing.T) {
	s := openHello(t)
	res, err := s.Query("SELECT name, type FROM sqlite_master WHERE type = 'view' ORDER BY name", 0)
	if err != nil {
		t.Fatalf("sqlite_master: %v", err)
	}
	have := map[string]bool{}
	for _, r := range res.Rows {
		have[r[0].(string)] = true
	}
	for _, v := range store.Views {
		if !have[v.Name] {
			t.Errorf("view %s missing", v.Name)
		}
	}
	// v_methods must resolve the Java class name without an explicit join.
	rows, err := s.Query("SELECT java_class, name, is_static FROM v_methods WHERE name = 'main'", 0)
	if err != nil {
		t.Fatalf("v_methods: %v", err)
	}
	if len(rows.Rows) != 1 {
		t.Fatalf("v_methods rows = %d", len(rows.Rows))
	}
	if got := rows.Rows[0][0]; got != "demo.Hello" {
		t.Fatalf("java_class = %v", got)
	}
	// v_overview is the one-query size summary.
	over, err := s.Query("SELECT row_count FROM v_overview WHERE relation = 'classes'", 0)
	if err != nil {
		t.Fatalf("v_overview: %v", err)
	}
	if over.Rows[0][0].(int64) != 1 {
		t.Fatalf("classes = %v", over.Rows[0][0])
	}
}

func TestSearchFindsEachKind(t *testing.T) {
	s := openHello(t)
	res, err := s.Search("Hello", nil, 20)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	kinds := map[string]bool{}
	for _, row := range res.Rows {
		kinds[row[0].(string)] = true
	}
	for _, want := range []string{"class", "method", "string"} {
		if !kinds[want] {
			t.Errorf("search did not return a %s row", want)
		}
	}
	// The exact class descriptor must be the first hit.
	if res.Rows[0][1] != "Ldemo/Hello;" {
		t.Errorf("first hit = %v", res.Rows[0][1])
	}
	if _, err := s.Search("Hello", []string{"nope"}, 5); err == nil {
		t.Error("unknown kind should fail")
	}
	if _, err := s.Search("", nil, 5); err == nil {
		t.Error("empty keyword should fail")
	}
	// LIKE metacharacters must be treated literally.
	res, err = s.Search("100%", nil, 5)
	if err != nil {
		t.Fatalf("search with %%: %v", err)
	}
	if len(res.Rows) != 0 {
		t.Errorf("expected no rows for 100%%, got %d", len(res.Rows))
	}
}

func TestRecipesRenderAndRun(t *testing.T) {
	s := openHello(t)
	callers, ok := store.FindRecipe("callers")
	if !ok {
		t.Fatal("callers recipe missing")
	}
	sqlText, err := callers.Render(map[string]string{"method": "Ljava/io/PrintStream;->println(Ljava/lang/String;)V"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(sqlText, ":method") {
		t.Fatalf("placeholder left in SQL: %s", sqlText)
	}
	res, err := s.Query(sqlText, 10)
	if err != nil {
		t.Fatalf("run callers: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0][0] != "Ldemo/Hello;->main([Ljava/lang/String;)V" {
		t.Fatalf("callers rows = %v", res.Rows)
	}
	// Missing required argument is an error, not a silently empty query.
	if _, err := callers.Render(nil); err == nil {
		t.Fatal("missing --arg should fail")
	}
	// Quotes in an argument must be escaped, not executed.
	evil, err := callers.Render(map[string]string{"method": "x'; DROP TABLE methods; --"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Query(evil, 10); err != nil {
		t.Fatalf("quoted argument should still parse: %v", err)
	}
}

func TestEveryRecipeIsValidSQL(t *testing.T) {
	s := openHello(t)
	for _, r := range store.Recipes {
		args := map[string]string{}
		for _, a := range r.Args {
			args[a.Name] = a.Default
			if args[a.Name] == "" {
				args[a.Name] = "%"
			}
		}
		sqlText, err := r.Render(args)
		if err != nil {
			t.Errorf("%s: %v", r.Name, err)
			continue
		}
		if _, err := s.Query(sqlText, 5); err != nil {
			t.Errorf("%s: %v\n%s", r.Name, err, sqlText)
		}
	}
}
