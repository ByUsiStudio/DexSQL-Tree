package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/byusi-studio/dexsql-tree/internal/api"
	"github.com/byusi-studio/dexsql-tree/internal/store"
)

func demoPath() string { return filepath.Join("..", "..", "testdata", "demo.apk") }

func newServer(t *testing.T, configure func(*api.Server)) (*api.Server, *httptest.Server, *store.Store) {
	t.Helper()
	s := api.New()
	s.Lazy = true
	if configure != nil {
		configure(s)
	}
	if _, st, err := s.Add(demoPath()); err != nil {
		t.Fatalf("add demo: %v", err)
	} else {
		ts := httptest.NewServer(s.Handler())
		t.Cleanup(func() {
			ts.Close()
			s.Close()
		})
		return s, ts, st
	}
	return nil, nil, nil
}
func getJSON(t *testing.T, url string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return res.StatusCode, body
}

func postJSON(t *testing.T, url string, in any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("content-type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return res.StatusCode, body
}

// A dataset opened over HTTP is empty until a statement asks for a table; the
// first query builds it and the next one reuses it.
func TestLazyQuery(t *testing.T) {
	_, ts, st := newServer(t, nil)
	if n := len(st.Tables()); n != 0 {
		t.Fatalf("lazy dataset built %d tables at open", n)
	}
	code, body := postJSON(t, ts.URL+"/api/query", map[string]any{
		"sql": "SELECT count(*) AS n FROM methods",
	}, nil)
	if code != 200 {
		t.Fatalf("query status %d: %v", code, body)
	}
	rows := body["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %v", rows)
	}
	if n := len(st.Tables()); n != 1 {
		t.Fatalf("built tables after one query = %v", st.Tables())
	}
	if code, _ := postJSON(t, ts.URL+"/api/query", map[string]any{
		"sql": "SELECT count(*) FROM method_calls",
	}, nil); code != 200 {
		t.Fatalf("second query status %d", code)
	}
	if len(st.Tables()) != 2 {
		t.Fatalf("built tables after two queries = %v", st.Tables())
	}
}

func TestHealthAndTables(t *testing.T) {
	_, ts, _ := newServer(t, nil)
	code, body := getJSON(t, ts.URL+"/api/health", nil)
	if code != 200 || body["ok"] != true {
		t.Fatalf("health = %d %v", code, body)
	}
	code, body = getJSON(t, ts.URL+"/api/tables", nil)
	if code != 200 || len(body["tables"].([]any)) < len(store.Catalog) {
		t.Fatalf("tables = %d", code)
	}
}

// An empty search result is a list, not null: callers should not have to
// special-case the empty case before iterating.
func TestSearchEmptyIsArray(t *testing.T) {
	_, ts, _ := newServer(t, nil)
	code, body := getJSON(t, ts.URL+"/api/search?keyword=zzzz-no-such-symbol", nil)
	if code != 200 {
		t.Fatalf("search status %d", code)
	}
	if body["rows"] == nil {
		t.Fatalf("rows is null: %v", body)
	}
	if rows := body["rows"].([]any); len(rows) != 0 {
		t.Fatalf("rows = %v", rows)
	}
}

func TestTokenRequired(t *testing.T) {
	_, ts, _ := newServer(t, func(s *api.Server) { s.Token = "s3cret" })
	if code, _ := getJSON(t, ts.URL+"/api/health", nil); code != 401 {
		t.Fatalf("health without token = %d", code)
	}
	if code, _ := getJSON(t, ts.URL+"/api/health", map[string]string{
		"Authorization": "Bearer wrong",
	}); code != 401 {
		t.Fatalf("wrong token = %d", code)
	}
	code, body := getJSON(t, ts.URL+"/api/health", map[string]string{
		"Authorization": "Bearer s3cret",
	})
	if code != 200 || body["ok"] != true {
		t.Fatalf("correct token = %d %v", code, body)
	}
	// The query parameter exists so the playground page can pass the token.
	if code, _ := getJSON(t, ts.URL+"/api/tables?token=s3cret", nil); code != 200 {
		t.Fatalf("token query parameter = %d", code)
	}
	// The static page stays reachable so a browser can load it.
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("playground page: %v", err)
	}
	res.Body.Close()
	if res.StatusCode == 401 {
		t.Fatalf("playground page should not require a token")
	}
}
