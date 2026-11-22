// Command dexsql-tree is a fully open-source SQL and call-tree explorer for
// Android APK/DEX files. It ships a CLI, an HTTP API and a stdio MCP server in
// one binary; the Java client in java/ talks to the HTTP API.
package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dexsql-tree/dexsql-tree/internal/api"
	"github.com/dexsql-tree/dexsql-tree/internal/dex"
	"github.com/dexsql-tree/dexsql-tree/internal/mcp"
	"github.com/dexsql-tree/dexsql-tree/internal/store"
	"github.com/dexsql-tree/dexsql-tree/internal/tree"
)

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	var err error
	switch cmd {
	case "query", "q":
		err = cmdQuery(args)
	case "tables":
		err = cmdTables(args)
	case "tree":
		err = cmdTree(args)
	case "smali":
		err = cmdSmali(args)
	case "serve":
		err = cmdServe(args)
	case "mcp":
		err = cmdMCP(args)
	case "version", "--version", "-v":
		fmt.Printf("dexsql-tree %s\n", api.Version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`dexsql-tree - SQL and call trees for Android APK/DEX files

Usage:
  dexsql-tree query  --input app.apk --sql "SELECT ..." [--limit N] [--format table|json|csv|md]
  dexsql-tree query  --input app.apk [--interactive]
  dexsql-tree tables [--json]
  dexsql-tree tree   --input app.apk --method 'Lcom/x;->y()V' [--direction callers|callees] [--depth N]
  dexsql-tree smali  --input app.apk --method 'Lcom/x;->y()V'
  dexsql-tree serve  --input app.apk [--addr 127.0.0.1:8765]
  dexsql-tree mcp    [--input app.apk]

Repeat --input to query several APK/DEX files together.
`)
}

func (m multiFlag) load() (*dex.Container, error) {
	if len(m) == 0 {
		return nil, fmt.Errorf("at least one --input is required")
	}
	var cs []*dex.Container
	for _, p := range m {
		c, err := dex.Load(p)
		if err != nil {
			return nil, err
		}
		for _, w := range c.Warnings {
			fmt.Fprintln(os.Stderr, "warning:", w)
		}
		cs = append(cs, c)
	}
	return dex.Merge(cs), nil
}

func cmdQuery(args []string) error {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	var inputs multiFlag
	fs.Var(&inputs, "input", "APK/DEX input (repeatable)")
	sqlText := fs.String("sql", "", "SQL statement")
	limit := fs.Int("limit", 0, "maximum rows (0 = all)")
	format := fs.String("format", "table", "table|json|csv|md")
	interactive := fs.Bool("interactive", false, "read SQL from stdin")
	fs.BoolVar(interactive, "repl", false, "alias of --interactive")
	output := fs.String("output", "", "write result to file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := inputs.load()
	if err != nil {
		return err
	}
	st, err := store.Open(c)
	if err != nil {
		return err
	}
	defer st.Close()

	run := func(q string) error {
		res, err := st.Query(q, *limit)
		if err != nil {
			return err
		}
		text, err := render(res, *format)
		if err != nil {
			return err
		}
		if *output != "" {
			return os.WriteFile(*output, []byte(text), 0o644)
		}
		fmt.Print(text)
		return nil
	}
	if *sqlText != "" {
		return run(*sqlText)
	}
	if !*interactive && !isTerminal() {
		// Batch mode: the whole stdin is one statement.
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(b)) == "" {
			return fmt.Errorf("no --sql and empty stdin")
		}
		return run(string(b))
	}
	return repl(st)
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func repl(st *store.Store) error {
	fmt.Println("dexsql-tree interactive session. End statements with ';'. Type .tables, .schema NAME or .quit.")
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	var buf strings.Builder
	for {
		if buf.Len() == 0 {
			fmt.Print("dexsql> ")
		} else {
			fmt.Print("   ...> ")
		}
		if !sc.Scan() {
			fmt.Println()
			return nil
		}
		line := sc.Text()
		trim := strings.TrimSpace(line)
		if buf.Len() == 0 {
			switch {
			case trim == ".quit" || trim == ".exit":
				return nil
			case trim == ".tables":
				for _, t := range store.Catalog {
					fmt.Println(t.Name)
				}
				continue
			case strings.HasPrefix(trim, ".schema"):
				name := strings.TrimSpace(strings.TrimPrefix(trim, ".schema"))
				if t, ok := store.CatalogByName[name]; ok {
					fmt.Println(strings.Join(t.Cols, "\n"))
				} else {
					fmt.Println("unknown table")
				}
				continue
			}
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
		if !strings.HasSuffix(trim, ";") {
			continue
		}
		q := buf.String()
		buf.Reset()
		res, err := st.Query(q, 200)
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		text, _ := render(res, "table")
		fmt.Print(text)
	}
}

func render(res *store.Result, format string) (string, error) {
	switch format {
	case "json":
		b, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b) + "\n", nil
	case "csv":
		var sb strings.Builder
		w := csv.NewWriter(&sb)
		w.Write(res.Columns)
		for _, row := range res.Rows {
			rec := make([]string, len(row))
			for i, v := range row {
				if v == nil {
					rec[i] = ""
				} else {
					rec[i] = fmt.Sprint(v)
				}
			}
			w.Write(rec)
		}
		w.Flush()
		return sb.String(), w.Error()
	case "md":
		return markdown(res), nil
	default:
		return table(res), nil
	}
}

func table(res *store.Result) string {
	if len(res.Rows) == 0 {
		return fmt.Sprintf("(0 rows, %d ms)\n", res.ElapsedMS)
	}
	widths := make([]int, len(res.Columns))
	for i, c := range res.Columns {
		widths[i] = runeLen(c)
	}
	cell := func(v any) string {
		if v == nil {
			return "NULL"
		}
		return fmt.Sprint(v)
	}
	for _, row := range res.Rows {
		for i, v := range row {
			if i >= len(widths) {
				continue
			}
			if n := runeLen(cell(v)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for i := range widths {
		if widths[i] > 64 {
			widths[i] = 64
		}
	}
	var b strings.Builder
	writeRow := func(vals []string) {
		for i, v := range vals {
			if i >= len(widths) {
				break
			}
			if runeLen(v) > widths[i] {
				v = truncate(v, widths[i])
			}
			b.WriteString(pad(v, widths[i]))
			if i < len(vals)-1 {
				b.WriteString("  ")
			}
		}
		b.WriteByte('\n')
	}
	writeRow(res.Columns)
	for i := range res.Columns {
		b.WriteString(strings.Repeat("-", widths[i]))
		if i < len(res.Columns)-1 {
			b.WriteString("  ")
		}
	}
	b.WriteByte('\n')
	for _, row := range res.Rows {
		vals := make([]string, len(row))
		for i, v := range row {
			vals[i] = cell(v)
		}
		writeRow(vals)
	}
	fmt.Fprintf(&b, "(%d rows, %d ms", len(res.Rows), res.ElapsedMS)
	if res.Truncated {
		b.WriteString(", truncated")
	}
	b.WriteString(")\n")
	return b.String()
}

func markdown(res *store.Result) string {
	var b strings.Builder
	b.WriteString("| " + strings.Join(res.Columns, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat(" --- |", len(res.Columns)) + "\n")
	for _, row := range res.Rows {
		cells := make([]string, len(row))
		for i, v := range row {
			if v == nil {
				cells[i] = "NULL"
			} else {
				cells[i] = strings.ReplaceAll(fmt.Sprint(v), "|", "\\|")
			}
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	fmt.Fprintf(&b, "\n%d rows, %d ms\n", len(res.Rows), res.ElapsedMS)
	return b.String()
}

func runeLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func pad(s string, n int) string {
	return s + strings.Repeat(" ", n-runeLen(s))
}

func cmdTables(args []string) error {
	fs := flag.NewFlagSet("tables", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *asJSON {
		b, _ := json.MarshalIndent(store.Catalog, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	for _, t := range store.Catalog {
		fmt.Printf("%-32s %s\n", t.Name, t.Doc)
	}
	return nil
}

func cmdTree(args []string) error {
	fs := flag.NewFlagSet("tree", flag.ContinueOnError)
	var inputs multiFlag
	fs.Var(&inputs, "input", "APK/DEX input (repeatable)")
	method := fs.String("method", "", "root method descriptor")
	direction := fs.String("direction", "callers", "callers|callees")
	depth := fs.Int("depth", 3, "maximum depth")
	maxNodes := fs.Int("maxNodes", 5000, "node budget")
	format := fs.String("format", "text", "text|json|dot")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *method == "" {
		return fmt.Errorf("--method is required")
	}
	c, err := inputs.load()
	if err != nil {
		return err
	}
	g := tree.BuildGraph(c)
	res, err := g.Build(*method, tree.Options{Direction: *direction, Depth: *depth, MaxNodes: *maxNodes})
	if err != nil {
		return err
	}
	switch *format {
	case "json":
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
	case "dot":
		fmt.Print(res.DOT())
	default:
		fmt.Print(res.Text())
	}
	return nil
}

func cmdSmali(args []string) error {
	fs := flag.NewFlagSet("smali", flag.ContinueOnError)
	var inputs multiFlag
	fs.Var(&inputs, "input", "APK/DEX input (repeatable)")
	method := fs.String("method", "", "method descriptor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *method == "" {
		return fmt.Errorf("--method is required")
	}
	c, err := inputs.load()
	if err != nil {
		return err
	}
	f, idx, ok := c.FindMethod(*method)
	if !ok {
		return fmt.Errorf("method not found: %s", *method)
	}
	text, err := f.Smali(idx)
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var inputs multiFlag
	fs.Var(&inputs, "input", "APK/DEX input (repeatable)")
	addr := fs.String("addr", "127.0.0.1:8765", "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s := api.New()
	if len(inputs) > 0 {
		c, err := inputs.load()
		if err != nil {
			return err
		}
		st, err := store.Open(c)
		if err != nil {
			return err
		}
		s.AddStore("d1", st)
	}
	return s.Serve(api.ParseAddr(*addr))
}

func cmdMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	var inputs multiFlag
	fs.Var(&inputs, "input", "APK/DEX input (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	srv := mcp.New(os.Stdout)
	for _, p := range inputs {
		if _, _, err := srv.Open(p); err != nil {
			return err
		}
	}
	return srv.Serve(os.Stdin)
}
