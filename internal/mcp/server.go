// Package mcp implements a small stdio MCP server (JSON-RPC 2.0, newline
// delimited) that exposes the SQL store and the call tree to AI agents.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/dexsql-tree/dexsql-tree/internal/dex"
	"github.com/dexsql-tree/dexsql-tree/internal/store"
	"github.com/dexsql-tree/dexsql-tree/internal/tree"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Server is a stdio MCP endpoint over one or more datasets.
type Server struct {
	mu      sync.Mutex
	stores  map[string]*store.Store
	graphs  map[string]*tree.Graph
	nextID  int
	out     io.Writer
	writeMu sync.Mutex
}

// New creates a server writing responses to out.
func New(out io.Writer) *Server {
	return &Server{stores: map[string]*store.Store{}, graphs: map[string]*tree.Graph{}, out: out}
}

// Open loads a dataset and returns its id.
func (s *Server) Open(path string) (string, *store.Store, error) {
	c, err := dex.Load(path)
	if err != nil {
		return "", nil, err
	}
	st, err := store.Open(c)
	if err != nil {
		return "", nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := fmt.Sprintf("d%d", s.nextID)
	s.stores[id] = st
	return id, st, nil
}

// defaultStore returns the requested dataset, or the only loaded one.
func (s *Server) defaultStore(id string) (*store.Store, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != "" {
		st, ok := s.stores[id]
		if !ok {
			return nil, "", fmt.Errorf("unknown dataset %q", id)
		}
		return st, id, nil
	}
	if len(s.stores) == 1 {
		for k, st := range s.stores {
			return st, k, nil
		}
	}
	if len(s.stores) == 0 {
		return nil, "", fmt.Errorf("no dataset open; call open_dataset first")
	}
	return nil, "", fmt.Errorf("multiple datasets open; pass dataset id")
}

// Serve runs the read loop until EOF.
func (s *Server) Serve(in io.Reader) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.respond(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		s.handle(req)
	}
	return sc.Err()
}

func (s *Server) respond(r rpcResponse) {
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.out.Write(append(b, '\n'))
}

func (s *Server) reply(id json.RawMessage, v any) {
	s.respond(rpcResponse{JSONRPC: "2.0", ID: id, Result: v})
}

func (s *Server) fail(id json.RawMessage, code int, format string, args ...any) {
	s.respond(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: fmt.Sprintf(format, args...)}})
}

func (s *Server) handle(req rpcRequest) {
	if len(req.ID) == 0 {
		// Notification: initialized / cancelled / progress. Nothing to answer.
		return
	}
	switch req.Method {
	case "initialize":
		s.reply(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{"name": "dexsql-tree", "version": "0.1.0"},
		})
	case "ping":
		s.reply(req.ID, map[string]any{})
	case "tools/list":
		s.reply(req.ID, map[string]any{"tools": toolDefs})
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			s.fail(req.ID, -32602, "invalid params")
			return
		}
		text, err := s.callTool(p.Name, p.Arguments)
		if err != nil {
			s.reply(req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": err.Error()}},
				"isError": true,
			})
			return
		}
		s.reply(req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": false,
		})
	default:
		s.fail(req.ID, -32601, "method not found: %s", req.Method)
	}
}

var toolDefs = []map[string]any{
	{
		"name":        "open_dataset",
		"description": "Open an APK, JAR, ZIP or DEX file and return a dataset id plus statistics. Parsing is local and read-only.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Absolute path to the input file."},
			},
			"required": []string{"path"},
		},
	},
	{
		"name":        "list_datasets",
		"description": "List datasets opened in this session.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
	},
	{
		"name":        "list_tables",
		"description": "List every SQL relation with its columns and purpose.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
	},
	{
		"name":        "describe_table",
		"description": "Describe one SQL relation: columns, types and meaning.",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
			"required":   []string{"name"},
		},
	},
	{
		"name":        "query",
		"description": "Run one read-only SQL statement over the loaded dataset. Returns columns, rows, rowCount and a truncated flag. Quote the column named \"index\" in method_refs.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"sql":     map[string]any{"type": "string", "description": "SQLite SELECT/WITH statement."},
				"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 5000},
				"dataset": map[string]any{"type": "string", "description": "Dataset id from open_dataset; optional when only one is open."},
			},
			"required": []string{"sql"},
		},
	},
	{
		"name":        "call_tree",
		"description": "Build a caller or callee tree for a method descriptor with cycle detection and a depth limit.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"method":    map[string]any{"type": "string", "description": "Method descriptor, e.g. Lcom/x/A;->b(I)V"},
				"direction": map[string]any{"type": "string", "enum": []string{"callers", "callees"}},
				"depth":     map[string]any{"type": "integer", "minimum": 1, "maximum": 32},
				"dataset":   map[string]any{"type": "string"},
			},
			"required": []string{"method"},
		},
	},
	{
		"name":        "smali",
		"description": "Disassemble one method to smali text.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"method":  map[string]any{"type": "string"},
				"dataset": map[string]any{"type": "string"},
			},
			"required": []string{"method"},
		},
	},
}

func (s *Server) callTool(name string, raw json.RawMessage) (string, error) {
	switch name {
	case "open_dataset":
		var a struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		id, st, err := s.Open(a.Path)
		if err != nil {
			return "", err
		}
		return jsonText(map[string]any{"dataset": id, "path": a.Path, "stats": st.Stats})
	case "list_datasets":
		s.mu.Lock()
		out := make([]map[string]any, 0, len(s.stores))
		for id, st := range s.stores {
			out = append(out, map[string]any{"dataset": id, "path": st.Container.Path, "stats": st.Stats})
		}
		s.mu.Unlock()
		return jsonText(out)
	case "list_tables":
		out := make([]map[string]any, 0, len(store.Catalog))
		for _, t := range store.Catalog {
			out = append(out, map[string]any{"name": t.Name, "columns": t.Columns(), "doc": t.Doc})
		}
		return jsonText(out)
	case "describe_table":
		var a struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		t, ok := store.CatalogByName[a.Name]
		if !ok {
			return "", fmt.Errorf("unknown table %q", a.Name)
		}
		return jsonText(t)
	case "query":
		var a struct {
			SQL     string `json:"sql"`
			Limit   int    `json:"limit"`
			Dataset string `json:"dataset"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		st, _, err := s.defaultStore(a.Dataset)
		if err != nil {
			return "", err
		}
		limit := a.Limit
		if limit <= 0 || limit > 5000 {
			limit = 200
		}
		res, err := st.Query(a.SQL, limit)
		if err != nil {
			return "", err
		}
		return jsonText(res)
	case "call_tree":
		var a struct {
			Method    string `json:"method"`
			Direction string `json:"direction"`
			Depth     int    `json:"depth"`
			Dataset   string `json:"dataset"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		st, id, err := s.defaultStore(a.Dataset)
		if err != nil {
			return "", err
		}
		s.mu.Lock()
		g := s.graphs[id]
		if g == nil {
			g = tree.BuildGraph(st.Container)
			s.graphs[id] = g
		}
		s.mu.Unlock()
		res, err := g.Build(a.Method, tree.Options{Direction: a.Direction, Depth: a.Depth})
		if err != nil {
			return "", err
		}
		return jsonText(res)
	case "smali":
		var a struct {
			Method  string `json:"method"`
			Dataset string `json:"dataset"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		st, _, err := s.defaultStore(a.Dataset)
		if err != nil {
			return "", err
		}
		f, idx, ok := st.Container.FindMethod(a.Method)
		if !ok {
			return "", fmt.Errorf("method not found: %s", a.Method)
		}
		return f.Smali(idx)
	}
	return "", fmt.Errorf("unknown tool %q", name)
}

func jsonText(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
