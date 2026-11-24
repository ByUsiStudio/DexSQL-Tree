package mcp_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byusi-studio/dexsql-tree/internal/mcp"
)

type response struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func run(t *testing.T, lines ...string) map[int]response {
	t.Helper()
	var out bytes.Buffer
	srv := mcp.New(&out)
	srv.Version = "test"
	if err := srv.Serve(strings.NewReader(strings.Join(lines, "\n") + "\n")); err != nil {
		t.Fatalf("serve: %v", err)
	}
	got := map[int]response{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var r response
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("bad response %q: %v", line, err)
		}
		got[r.ID] = r
	}
	return got
}

func rpc(id int, method, params string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, id, method, params)
}

func TestInitializeAndTools(t *testing.T) {
	got := run(t, rpc(1, "initialize", `{}`), rpc(2, "tools/list", `{}`))
	if got[1].Error != nil {
		t.Fatalf("initialize: %+v", got[1].Error)
	}
	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(got[1].Result, &init); err != nil {
		t.Fatalf("decode initialize: %v", err)
	}
	if init.ServerInfo.Name != "dexsql-tree" || init.ProtocolVersion == "" {
		t.Fatalf("initialize = %+v", init)
	}
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(got[2].Result, &list); err != nil {
		t.Fatalf("decode tools: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range list.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"open_dataset", "query", "search", "run_recipe", "call_tree"} {
		if !names[want] {
			t.Errorf("tool %s missing", want)
		}
	}
}

func TestOpenThenQuery(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", "testdata", "demo.apk"))
	if err != nil {
		t.Fatal(err)
	}
	got := run(t,
		rpc(1, "tools/call", `{"name":"open_dataset","arguments":{"path":`+quote(path)+`}}`),
		rpc(2, "tools/call", `{"name":"query","arguments":{"sql":"SELECT count(*) AS n FROM methods"}}`),
		rpc(3, "tools/call", `{"name":"query","arguments":{"sql":"SELECT count(*) AS n FROM method_calls"}}`),
	)
	for _, id := range []int{1, 2, 3} {
		if got[id].Error != nil {
			t.Fatalf("call %d failed: %+v", id, got[id].Error)
		}
	}
	var open struct {
		Dataset string `json:"dataset"`
		Stats   struct {
			Methods int `json:"methods"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(contentText(t, got[1]), &open); err != nil {
		t.Fatalf("decode open: %v", err)
	}
	if open.Dataset != "d1" || open.Stats.Methods != 2 {
		t.Fatalf("open = %+v", open)
	}
	for id, want := range map[int]float64{2: 2, 3: 1} {
		var res struct {
			Rows [][]any `json:"rows"`
		}
		if err := json.Unmarshal(contentText(t, got[id]), &res); err != nil {
			t.Fatalf("decode result %d: %v", id, err)
		}
		if len(res.Rows) != 1 || res.Rows[0][0].(float64) != want {
			t.Fatalf("call %d rows = %v", id, res.Rows)
		}
	}
}

func TestUnknownMethod(t *testing.T) {
	got := run(t, rpc(1, "nope", `{}`))
	if got[1].Error == nil || got[1].Error.Code != -32601 {
		t.Fatalf("unknown method = %+v", got[1])
	}
}

// contentText unwraps the MCP tools/call envelope: result.content[0].text.
func contentText(t *testing.T, r response) []byte {
	t.Helper()
	var env struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(r.Result, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(env.Content) == 0 {
		t.Fatalf("empty content")
	}
	return []byte(env.Content[0].Text)
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
