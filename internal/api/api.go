package api

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/byusi-studio/dexsql-tree/internal/cache"
	"github.com/byusi-studio/dexsql-tree/internal/dex"
	"github.com/byusi-studio/dexsql-tree/internal/i18n"
	"github.com/byusi-studio/dexsql-tree/internal/store"
	"github.com/byusi-studio/dexsql-tree/internal/tree"
)

//go:embed index.html
var indexHTML []byte

// Server exposes datasets over HTTP for the Java client and the built-in
// playground. It is deliberately small: JSON in, JSON out, no auth.
type Server struct {
	// Debug materialises DEX line-number tables for datasets opened through the
	// HTTP API. It is off by default because debug programs can dominate load
	// time and memory.
	Debug bool
	// Disk materialises datasets in a temporary file instead of memory.
	Disk bool
	// Profile selects fast or full materialisation. Empty means fast.
	Profile store.Profile
	// Cache enables the persistent catalog cache for datasets opened later.
	Cache bool
	// CacheDir overrides the cache location; empty means the user cache dir.
	CacheDir string
	mu       sync.Mutex
	stores   map[string]*store.Store
	graphs   map[string]*tree.Graph
	nextID   int
}

// New returns an empty server.
func New() *Server {
	return &Server{stores: map[string]*store.Store{}, graphs: map[string]*tree.Graph{}}
}

// Close releases every dataset opened by the server, including temporary
// on-disk catalogs created with --disk.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.stores {
		st.Close()
	}
	s.stores = map[string]*store.Store{}
	s.graphs = map[string]*tree.Graph{}
}

// AddStore registers an already-opened store under id.
func (s *Server) AddStore(id string, st *store.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stores[id] = st
	if n, err := strconv.Atoi(strings.TrimPrefix(id, "d")); err == nil && n > s.nextID {
		s.nextID = n
	}
}

// Add loads a path and registers it.
func (s *Server) Add(path string) (string, *store.Store, error) {
	return s.AddJobs(path, 0)
}

// AddJobs is Add with an explicit parallel decoder count.
func (s *Server) AddJobs(path string, jobs int) (string, *store.Store, error) {
	c, err := dex.Load(path, dex.Options{Debug: s.Debug, Jobs: jobs})
	if err != nil {
		return "", nil, err
	}
	profile := s.Profile
	if profile == "" {
		profile = store.ProfileFast
	}
	opt := store.Options{Disk: s.Disk, Profile: profile}
	if s.Cache {
		dir := s.CacheDir
		if dir == "" {
			dir = cache.Dir()
		}
		opt.CacheDir = dir
		opt.CacheKey = cache.Key([]string{path}, string(profile), Version, s.Debug)
	}
	st, err := store.Open(c, opt)
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

// Handler builds the HTTP mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"ok": true, "version": Version, "lang": i18n.Lang(),
			"profile": string(s.Profile), "cache": s.Cache,
		})
	})
	mux.HandleFunc("/api/datasets", s.handleDatasets)
	mux.HandleFunc("/api/tables", s.handleTables)
	mux.HandleFunc("/api/recipes", s.handleRecipes)
	mux.HandleFunc("/api/table", s.handleTable)
	mux.HandleFunc("/api/query", s.handleQuery)
	mux.HandleFunc("/api/tree", s.handleTree)
	mux.HandleFunc("/api/smali", s.handleSmali)
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/cache", s.handleCache)
	return cors(mux)
}

// Version is reported by /api/health and the CLI.
var Version = "0.3.0"

func cors(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "content-type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

type datasetInfo struct {
	ID       string    `json:"id"`
	Path     string    `json:"path"`
	Stats    dex.Stats `json:"stats"`
	Warnings []string  `json:"warnings,omitempty"`
}

func (s *Server) handleDatasets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		out := make([]datasetInfo, 0, len(s.stores))
		for id, st := range s.stores {
			out = append(out, datasetInfo{ID: id, Path: st.Container.Path, Stats: st.Stats, Warnings: st.Warnings})
		}
		s.mu.Unlock()
		writeJSON(w, 200, map[string]any{"datasets": out})
	case http.MethodPost:
		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, err)
			return
		}
		id, st, err := s.Add(req.Path)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, 200, datasetInfo{ID: id, Path: st.Container.Path, Stats: st.Stats, Warnings: st.Warnings})
	default:
		w.WriteHeader(405)
	}
}

func (s *Server) handleTables(w http.ResponseWriter, r *http.Request) {
	rels := store.AllRelations()
	out := make([]map[string]any, 0, len(rels))
	for _, t := range rels {
		out = append(out, map[string]any{"name": t.Name, "kind": t.Kind, "cols": t.Cols, "doc": t.Doc})
	}
	writeJSON(w, 200, map[string]any{"tables": out})
}

// handleRecipes lists the built-in query templates, or explains one by name.
func (s *Server) handleRecipes(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeJSON(w, 200, map[string]any{"recipes": store.Recipes})
		return
	}
	rec, ok := store.FindRecipe(name)
	if !ok {
		writeErr(w, 404, fmt.Errorf("unknown recipe %q", name))
		return
	}
	writeJSON(w, 200, rec)
}

func (s *Server) handleTable(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	t, ok := store.Relation(name)
	if !ok {
		writeErr(w, 404, fmt.Errorf("unknown table %q", name))
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) lookup(id string) (*store.Store, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" && len(s.stores) == 1 {
		for _, st := range s.stores {
			return st, nil
		}
	}
	st, ok := s.stores[id]
	if !ok {
		return nil, fmt.Errorf("unknown dataset %q", id)
	}
	return st, nil
}

const maxLimit = 5000

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var req struct {
		Dataset string            `json:"dataset"`
		SQL     string            `json:"sql"`
		Recipe  string            `json:"recipe"`
		Args    map[string]string `json:"args"`
		Limit   int               `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	st, err := s.lookup(req.Dataset)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	if req.Recipe != "" {
		if req.SQL != "" {
			writeErr(w, 400, fmt.Errorf("use either recipe or sql, not both"))
			return
		}
		rec, ok := store.FindRecipe(req.Recipe)
		if !ok {
			writeErr(w, 404, fmt.Errorf("unknown recipe %q", req.Recipe))
			return
		}
		sqlText, err := rec.Render(req.Args)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		req.SQL = sqlText
	}
	limit := req.Limit
	if limit <= 0 || limit > maxLimit {
		limit = 1000
	}
	res, err := st.Query(req.SQL, limit)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var req struct {
		Dataset   string `json:"dataset"`
		Method    string `json:"method"`
		Direction string `json:"direction"`
		Depth     int    `json:"depth"`
		MaxNodes  int    `json:"maxNodes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	st, err := s.lookup(req.Dataset)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	s.mu.Lock()
	g := s.graphs[st.Container.Path]
	if g == nil {
		g = tree.BuildGraph(st.Container)
		s.graphs[st.Container.Path] = g
	}
	s.mu.Unlock()
	res, err := g.Build(req.Method, tree.Options{Direction: req.Direction, Depth: req.Depth, MaxNodes: req.MaxNodes})
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleSmali(w http.ResponseWriter, r *http.Request) {
	desc := r.URL.Query().Get("method")
	if desc == "" {
		writeErr(w, 400, fmt.Errorf("method is required"))
		return
	}
	id := r.URL.Query().Get("dataset")
	st, err := s.lookup(id)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	f, idx, ok := st.Container.FindMethod(desc)
	if !ok {
		writeErr(w, 404, fmt.Errorf("method not found: %s", desc))
		return
	}
	text, err := f.Smali(idx)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"method": desc, "smali": text})
}

// Serve starts the HTTP server on addr and blocks.
func (s *Server) Serve(addr string) error {
	log.Printf("dexsql-tree %s listening on http://%s", Version, addr)
	return http.ListenAndServe(addr, s.Handler())
}

// ParseAddr validates "host:port" and returns it with a default port.
func ParseAddr(addr string) string {
	if addr == "" {
		return "127.0.0.1:8765"
	}
	if !strings.Contains(addr, ":") {
		return "127.0.0.1:" + addr
	}
	if _, err := strconv.Atoi(addr[strings.LastIndex(addr, ":")+1:]); err != nil {
		return "127.0.0.1:8765"
	}
	return addr
}

// handleSearch answers keyword lookups without SQL. It scans the parsed model,
// so it works even when the catalog was served from cache.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kw := strings.TrimSpace(q.Get("keyword"))
	if kw == "" {
		writeErr(w, 400, fmt.Errorf("keyword is required"))
		return
	}
	st, err := s.lookup(q.Get("dataset"))
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	var kinds []string
	for _, raw := range q["kind"] {
		for _, k := range strings.Split(raw, ",") {
			if k = strings.TrimSpace(k); k != "" {
				kinds = append(kinds, k)
			}
		}
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > maxLimit {
		limit = 100
	}
	res, err := st.Search(kw, kinds, limit)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, res)
}

// handleCache reports the catalog cache and lets a client clear it.
func (s *Server) handleCache(w http.ResponseWriter, r *http.Request) {
	dir := s.CacheDir
	if dir == "" {
		dir = cache.Dir()
	}
	switch r.Method {
	case http.MethodGet:
		ents, err := cache.List(dir)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		var total int64
		for _, e := range ents {
			total += e.Size
		}
		writeJSON(w, 200, map[string]any{
			"dir": dir, "enabled": s.Cache, "entries": ents,
			"count": len(ents), "bytes": total,
		})
	case http.MethodDelete:
		n, b, err := cache.Clear(dir)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]any{"removed": n, "bytes": b})
	default:
		w.WriteHeader(405)
	}
}
