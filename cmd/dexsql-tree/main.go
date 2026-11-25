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
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/byusi-studio/dexsql-tree/internal/api"
	"github.com/byusi-studio/dexsql-tree/internal/cache"
	"github.com/byusi-studio/dexsql-tree/internal/dex"
	"github.com/byusi-studio/dexsql-tree/internal/i18n"
	"github.com/byusi-studio/dexsql-tree/internal/mcp"
	"github.com/byusi-studio/dexsql-tree/internal/store"
	"github.com/byusi-studio/dexsql-tree/internal/tree"
)

type multiFlag struct {
	paths    []string
	debug    bool
	jobs     int
	profile  string
	cache    bool
	cacheDir string
}

func (m *multiFlag) String() string { return strings.Join(m.paths, ",") }
func (m *multiFlag) Set(v string) error {
	m.paths = append(m.paths, v)
	return nil
}

// listFlag collects a repeatable string option.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// kvFlag collects repeatable name=value options for recipes.
type kvFlag map[string]string

func (k kvFlag) String() string {
	keys := make([]string, 0, len(k))
	for key := range k {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = key + "=" + k[key]
	}
	return strings.Join(parts, ",")
}

func (k *kvFlag) Set(v string) error {
	i := strings.IndexByte(v, '=')
	if i <= 0 {
		return fmt.Errorf("expected name=value, got %q", v)
	}
	if *k == nil {
		*k = kvFlag{}
	}
	(*k)[v[:i]] = v[i+1:]
	return nil
}

func main() {
	args, err := i18n.ConsumeLangFlag(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "start", "tour":
		err = cmdStart(rest)
	case "query", "q":
		err = cmdQuery(rest)
	case "search", "find":
		err = cmdSearch(rest)
	case "stats", "info":
		err = cmdStats(rest)
	case "recipes":
		err = cmdRecipes(rest)
	case "tables", "ls":
		err = cmdTables(rest)
	case "tree":
		err = cmdTree(rest)
	case "smali":
		err = cmdSmali(rest)
	case "serve":
		err = cmdServe(rest)
	case "mcp":
		err = cmdMCP(rest)
	case "cache":
		err = cmdCache(rest)
	case "lang", "language", "i18n":
		err = cmdLang(rest)
	case "version", "--version", "-v":
		fmt.Printf("dexsql-tree %s\n", api.Version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintln(os.Stderr, i18n.T(fmt.Sprintf("unknown command %q", cmd), fmt.Sprintf("未知命令 %q", cmd)))
		fmt.Fprintln(os.Stderr)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("error:", "错误:")+" "+err.Error())
		os.Exit(1)
	}
}

const usageEN = `dexsql-tree - SQL and call trees for Android APK/DEX files

Usage:
  dexsql-tree start   --input app.apk           first look: size plus three queries
  dexsql-tree query   --input app.apk --sql "SELECT ..." [--limit 200] [--format table|json|csv|md]
  dexsql-tree query   --input app.apk --recipe callers --arg method='Lcom/x;->y()V'
  dexsql-tree search  --input app.apk --keyword password [--kind class|method|field|string]
  dexsql-tree stats   --input app.apk [--json] [--top 5]
  dexsql-tree recipes [name]                    list or explain built-in queries
  dexsql-tree tables  [--json]                  tables and convenience views
  dexsql-tree tree    --input app.apk --method 'Lcom/x;->y()V' [--direction callers|callees]
  dexsql-tree smali   --input app.apk --method 'Lcom/x;->y()V'
  dexsql-tree serve   --input app.apk [--addr 127.0.0.1:8765] [--token SECRET]
  dexsql-tree mcp     [--input app.apk]
  dexsql-tree cache   list|clear|prune|path|info manage the persisted catalog cache
  dexsql-tree lang    set zh --write            enable Chinese prompts permanently

Language:
  DEXSQL_TREE_ZH=true|false  (DEXSQL-TREE_ZH and DEXSQL_TREE_LANG=zh|en also work)
  --lang zh|en               overrides the environment for one invocation.

Options:
  --cache  reuse the catalog built for this input (reopening drops from
           seconds to milliseconds; keyed by path, size, mtime and settings)
  --profile fast|full
           fast (default) skips the annotation tables; full materialises all
  --jobs N decode N DEX files in parallel (default: one per CPU)
  --debug  materialise DEX line-number tables (off by default: debug programs
           can be larger than the code itself)
  --disk   keep the catalog in a temporary file to reduce memory
  --all-tables
           materialise every table instead of only the ones the SQL reads
  --token SECRET (serve)
           require the bearer token on /api routes
Repeat --input to query several APK/DEX files together.
`

const usageZH = `dexsql-tree —— Android APK/DEX 的 SQL 查询与调用树浏览器

用法:
  dexsql-tree start   --input app.apk           首次查看：规模加三条查询
  dexsql-tree query   --input app.apk --sql "SELECT ..." [--limit 200] [--format table|json|csv|md]
  dexsql-tree query   --input app.apk --recipe callers --arg method='Lcom/x;->y()V'
  dexsql-tree search  --input app.apk --keyword 密码 [--kind class|method|field|string]
  dexsql-tree stats   --input app.apk [--json] [--top 5]
  dexsql-tree recipes [名称]                   列出或解释内置查询模板
  dexsql-tree tables  [--json]                 列出数据表与便捷视图
  dexsql-tree tree    --input app.apk --method 'Lcom/x;->y()V' [--direction callers|callees]
  dexsql-tree smali   --input app.apk --method 'Lcom/x;->y()V'
  dexsql-tree serve   --input app.apk [--addr 127.0.0.1:8765] [--token SECRET]
  dexsql-tree mcp     [--input app.apk]
  dexsql-tree cache   list|clear|prune|path|info 管理磁盘目录缓存
  dexsql-tree lang    set zh --write            永久开启中文提示

语言:
  DEXSQL_TREE_ZH=true|false（也支持 DEXSQL-TREE_ZH 与 DEXSQL_TREE_LANG=zh|en）
  --lang zh|en              只对本次调用生效。

可选:
  --cache  复用该输入已生成的目录（再次打开从数秒降到毫秒级；
           键由路径、大小、修改时间与设置共同决定）
  --profile fast|full
           fast（默认）跳过注解表，full 物化全部内容
  --jobs N 并行解析 N 个 DEX（默认等于 CPU 核数）
  --debug  物化 DEX 行号表（默认关闭：调试信息可能比代码本身还大）
  --disk   将目录放在临时文件中，降低内存占用
  --all-tables
           物化全部表，而不是只物化 SQL 用到的表
  --token SECRET（serve）
           要求 /api 路由携带该 token
重复 --input 可同时查询多个 APK/DEX 文件。
`

// tracef writes a diagnostic line to stderr when DEXSQL_TREE_VERBOSE is set.
// stdout stays machine-readable, so this never corrupts JSON output.
func tracef(format string, args ...any) {
	if os.Getenv("DEXSQL_TREE_VERBOSE") == "" {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func usage() {
	if i18n.On() {
		fmt.Print(usageZH)
		return
	}
	fmt.Print(usageEN)
}

func (m multiFlag) load() (*dex.Container, error) {
	if len(m.paths) == 0 {
		return nil, fmt.Errorf(i18n.T("at least one --input is required", "至少需要一个 --input 参数"))
	}
	var cs []*dex.Container
	for _, p := range m.paths {
		c, err := dex.Load(p, dex.Options{Debug: m.debug, Jobs: m.jobs})
		if err != nil {
			return nil, err
		}
		for _, w := range c.Warnings {
			fmt.Fprintln(os.Stderr, i18n.T("warning:", "警告:")+" "+w)
		}
		cs = append(cs, c)
	}
	return dex.Merge(cs), nil
}

func addInputFlags(fs *flag.FlagSet, inputs *multiFlag) {
	fs.Var(inputs, "input", i18n.T("APK/DEX input (repeatable)", "APK/DEX 输入文件（可重复）"))
	fs.BoolVar(&inputs.debug, "debug", false, i18n.T(
		"materialise DEX line-number tables (large and slow)",
		"物化 DEX 行号表（体积大、速度慢）"))
	fs.IntVar(&inputs.jobs, "jobs", 0, i18n.T(
		"parallel DEX decoders (default: CPU count)",
		"并行解析的 DEX 数量（默认等于 CPU 核数）"))
	fs.StringVar(&inputs.profile, "profile", "fast", i18n.T(
		"fast skips annotation tables, full materialises everything",
		"fast 跳过注解表，full 物化全部内容"))
	fs.BoolVar(&inputs.cache, "cache", false, i18n.T(
		"reuse the on-disk catalog for this input",
		"复用该输入已生成的磁盘目录"))
	fs.StringVar(&inputs.cacheDir, "cache-dir", "", i18n.T(
		"cache directory (default: user cache dir)",
		"缓存目录（默认使用系统缓存目录）"))
}

// storeOptions turns the parsed flags into a materialisation request, wiring up
// the persistent cache when --cache is set.
func (m *multiFlag) storeOptions(disk bool) (store.Options, error) {
	profile, err := store.ParseProfile(m.profile)
	if err != nil {
		return store.Options{}, err
	}
	opt := store.Options{Disk: disk, Profile: profile, Version: api.Version}
	if m.cache {
		dir := m.cacheDir
		if dir == "" {
			dir = cache.Dir()
		}
		opt.CacheDir = dir
		opt.CacheKey = cache.Key(m.paths, string(profile), api.Version, m.debug)
	}
	return opt, nil
}

// cmdStart is the first-run experience: size the input, ask three questions
// that are worth asking of any APK, and end each block with a command that goes
// deeper. It is not a tutorial — every suggestion is copyable.
func cmdStart(args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	var inputs multiFlag
	addInputFlags(fs, &inputs)
	limit := fs.Int("limit", 6, i18n.T("rows per example", "每个示例的行数"))
	format := fs.String("format", "table", i18n.T("table|json|csv|md", "table|json|csv|md"))
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(inputs.paths) == 0 {
		return fmt.Errorf(i18n.T("start needs --input app.apk", "start 需要 --input app.apk"))
	}
	c, err := inputs.load()
	if err != nil {
		return err
	}
	opt, err := inputs.storeOptions(false)
	if err != nil {
		return err
	}
	// Lazy: the three examples pay only for the tables they read.
	opt.Lazy = true
	st, err := store.Open(c, opt)
	if err != nil {
		return err
	}
	defer st.Close()

	path := inputs.paths[0]
	s := st.Stats
	fmt.Printf("%s  %s · %d DEX · %s %s · %s %s · %s %s\n\n",
		filepath.Base(path), humanBytes(s.DexBytes), s.Files,
		thousands(s.Methods), i18n.T("methods", "方法"),
		thousands(s.Classes), i18n.T("classes", "类"),
		thousands(s.Strings), i18n.T("strings", "字符串"))

	examples := []struct {
		title string
		sql   string
		next  string
	}{
		{
			title: i18n.T("Which classes carry the most methods?", "哪些类的方法最多？"),
			// class_methods counts what the class itself declares; methods would
			// rank framework classes by how often they are referenced.
			sql: `SELECT class_descriptor AS class, count(*) AS methods
FROM class_methods GROUP BY class_descriptor ORDER BY methods DESC LIMIT ` + strconv.Itoa(*limit),
			next: i18n.T("dexsql-tree tables class_methods   # what else this table holds",
				"dexsql-tree tables class_methods   # 这张表还有哪些列"),
		},
		{
			title: i18n.T("Is anything hardcoded in the strings?", "字符串里有没有写死的东西？"),
			// One row per answer: a multi-line HTML blob would break the table
			// and tell a newcomer nothing.
			sql: `SELECT substr(replace(replace(value, char(10), ' '), char(13), ' '), 1, 100) AS value
FROM strings
WHERE (value LIKE '%://%' OR value LIKE '%@%.%') AND length(value) < 200
GROUP BY value ORDER BY length(value) DESC LIMIT ` + strconv.Itoa(*limit),
			next: i18n.T(`dexsql-tree query --input `+path+` --sql "SELECT method, value FROM method_strings WHERE value LIKE '%token%' LIMIT 20"`,
				`dexsql-tree query --input `+path+` --sql "SELECT method, value FROM method_strings WHERE value LIKE '%token%' LIMIT 20"`),
		},
		{
			title: i18n.T("Where does execution start?", "执行从哪里开始？"),
			// Framework classes answer every one of these names; hide them or
			// the list is Android's own boilerplate.
			sql: `SELECT class_descriptor AS class, name FROM methods
WHERE name IN ('onCreate','main','onStartCommand','onReceive','attachBaseContext')
  AND class_descriptor NOT LIKE 'Landroid/%'
  AND class_descriptor NOT LIKE 'Landroidx/%'
  AND class_descriptor NOT LIKE 'Ljava/%'
  AND class_descriptor NOT LIKE 'Lkotlin/%'
ORDER BY name LIMIT ` + strconv.Itoa(*limit),
			next: i18n.T("dexsql-tree tree --input "+path+" --method '<descriptor>' --depth 2",
				"dexsql-tree tree --input "+path+" --method '<descriptor>' --depth 2"),
		},
	}

	for i, ex := range examples {
		fmt.Printf("%d. %s\n", i+1, ex.title)
		res, err := st.Query(ex.sql, *limit)
		if err != nil {
			// An example that fails must not abort the tour.
			fmt.Printf("   %v\n\n", store.Suggest(err))
			continue
		}
		text, err := render(res, *format)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			fmt.Printf("   %s\n", line)
		}
		fmt.Printf("   %s %s\n\n", i18n.T("next:", "接着试:"), ex.next)
	}

	fmt.Print(i18n.T(
		"Add --cache to reopen in under a second. `dexsql-tree recipes` lists every built-in query,\n"+
			"`dexsql-tree tables` explains what each relation holds.\n",
		"加 --cache 后再次打开不到一秒。`dexsql-tree recipes` 列出全部内置模板，\n"+
			"`dexsql-tree tables` 说明每张表放的是什么。\n"))
	return nil
}

// thousands renders 64586 as 64,586; the size line is the first thing a
// newcomer reads and a raw count is hard to size at a glance.
func thousands(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func cmdQuery(args []string) error {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	var inputs multiFlag
	addInputFlags(fs, &inputs)
	sqlText := fs.String("sql", "", i18n.T("SQL statement", "SQL 语句"))
	limit := fs.Int("limit", 200, i18n.T("maximum rows (0 = all)", "最大行数（0 表示全部）"))
	format := fs.String("format", "table", i18n.T("table|json|csv|md", "table|json|csv|md"))
	interactive := fs.Bool("interactive", false, i18n.T("read SQL from stdin", "从标准输入读取 SQL"))
	fs.BoolVar(interactive, "repl", false, i18n.T("alias of --interactive", "--interactive 的别名"))
	output := fs.String("output", "", i18n.T("write result to file", "将结果写入文件"))
	disk := fs.Bool("disk", false, i18n.T(
		"use a temporary on-disk catalog to reduce memory", "使用临时文件目录以降低内存占用"))
	recipe := fs.String("recipe", "", i18n.T("run a built-in recipe (see: dexsql-tree recipes)", "执行内置查询模板（见 dexsql-tree recipes）"))
	allTables := fs.Bool("all-tables", false, i18n.T(
		"materialise every table instead of only the ones the SQL reads",
		"物化全部表，而不是只物化 SQL 用到的表"))
	var kv kvFlag
	fs.Var(&kv, "arg", i18n.T("recipe argument name=value (repeatable)", "模板参数 name=value（可重复）"))
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *recipe != "" {
		if *sqlText != "" {
			return fmt.Errorf(i18n.T("use either --recipe or --sql, not both", "--recipe 与 --sql 不能同时使用"))
		}
		r, ok := store.FindRecipe(*recipe)
		if !ok {
			return fmt.Errorf(i18n.T("unknown recipe %q (run: dexsql-tree recipes)", "未知模板 %q（可运行 dexsql-tree recipes 查看）"), *recipe)
		}
		text, err := r.Render(kv)
		if err != nil {
			return err
		}
		*sqlText = text
	}
	// The statement is known before the container is, which is what lets the
	// catalog be built for the few tables this query reads. Interactive mode is
	// the exception: the next statement can name anything, so it builds all.
	statement := *sqlText
	if statement == "" && !*interactive && !isTerminal() {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		statement = string(b)
		if strings.TrimSpace(statement) == "" {
			return fmt.Errorf(i18n.T("no --sql and empty stdin", "没有 --sql，且标准输入为空"))
		}
	}
	var tables []string
	switch {
	case *allTables:
		tables = store.AllTables()
		tracef("plan: every table (%d)", len(tables))
	case strings.TrimSpace(statement) != "":
		planned, resolved := store.TablesForSQL(statement)
		tables = planned
		if resolved {
			tracef("plan: %d table(s): %s", len(planned), strings.Join(planned, ", "))
		} else {
			tracef("plan: this statement reads no catalog table")
		}
	default:
		// Interactive: the next statement can name anything, including the
		// derived tables the fast profile leaves out by default.
		tables = store.AllTables()
	}
	c, err := inputs.load()
	if err != nil {
		return err
	}
	opt, err := inputs.storeOptions(*disk)
	if err != nil {
		return err
	}
	if tables != nil {
		opt.Tables = tables
	}
	st, err := store.Open(c, opt)
	if err != nil {
		return err
	}
	defer st.Close()
	if opt.CacheDir != "" {
		tracef("catalog: %s (profile %s, %d methods, %d tables)",
			map[bool]string{true: "cache hit", false: "materialised"}[st.Cached],
			st.Profile, st.Stats.Methods, len(st.Tables()))
	}
	// A SQL-only process never touches the parsed model again; dropping it
	// before the query keeps the peak resident set near the catalog size.
	if st.Container != nil {
		st.ReleaseModel()
		c = nil
		runtime.GC()
	}

	run := func(q string) error {
		res, err := st.Query(q, *limit)
		if err != nil {
			return store.Suggest(err)
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
		return run(statement)
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

// loopbackAddr reports whether an address only accepts local connections.
func loopbackAddr(addr string) bool {
	host := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host = addr[:i]
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "localhost" || host == "::1" || host == "0:0:0:0:0:0:0:1" {
		return true
	}
	return strings.HasPrefix(host, "127.")
}

func repl(st *store.Store) error {
	fmt.Println(i18n.T(
		"dexsql-tree interactive session. End statements with ';'. Type .tables, .schema NAME or .quit.",
		"dexsql-tree 交互模式。语句以 ';' 结束。可输入 .tables、.schema 表名 或 .quit。"))
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	var buf strings.Builder
	for {
		if buf.Len() == 0 {
			fmt.Print(i18n.T("dexsql> ", "dexsql> "))
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
				for _, t := range store.AllRelations() {
					fmt.Println(t.Name)
				}
				continue
			case strings.HasPrefix(trim, ".schema"):
				name := strings.TrimSpace(strings.TrimPrefix(trim, ".schema"))
				if t, ok := store.Relation(name); ok {
					fmt.Println(strings.Join(t.Cols, "\n"))
				} else {
					fmt.Println(i18n.T("unknown table", "未知数据表"))
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
			fmt.Println(i18n.T("error:", "错误:") + " " + store.Suggest(err).Error())
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
		return fmt.Sprintf(i18n.T("(0 rows, %d ms)\n", "（0 行，%d 毫秒）\n"), res.ElapsedMS)
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
	fmt.Fprintf(&b, i18n.T("(%d rows, %d ms", "（%d 行，%d 毫秒"), len(res.Rows), res.ElapsedMS)
	if res.Truncated {
		b.WriteString(i18n.T(", truncated", "，已截断"))
	}
	b.WriteString(i18n.T(")\n", "）\n"))
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
	fmt.Fprintf(&b, "\n"+i18n.T("%d rows, %d ms\n", "%d 行，%d 毫秒\n"), len(res.Rows), res.ElapsedMS)
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

// padDisplay pads to a terminal width, counting CJK runes as two columns so
// bilingual labels line up.
func padDisplay(s string, width int) string {
	w := 0
	for _, r := range s {
		if r >= 0x1100 && (r <= 0x115F || (r >= 0x2E80 && r <= 0xA4CF) ||
			(r >= 0xAC00 && r <= 0xD7A3) || (r >= 0xF900 && r <= 0xFAFF) ||
			(r >= 0xFE30 && r <= 0xFE6F) || (r >= 0xFF00 && r <= 0xFF60) ||
			(r >= 0xFFE0 && r <= 0xFFE6)) {
			w += 2
		} else {
			w++
		}
	}
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// cmdSearch answers "where is this string or name" without any SQL.
func cmdSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	var inputs multiFlag
	addInputFlags(fs, &inputs)
	keyword := fs.String("keyword", "", i18n.T("text to search for", "要搜索的文本"))
	var kinds listFlag
	fs.Var(&kinds, "kind", i18n.T("class|method|field|string (repeatable, default all)", "class|method|field|string（可重复，默认全部）"))
	limit := fs.Int("limit", 50, i18n.T("maximum rows", "最大行数"))
	format := fs.String("format", "table", i18n.T("table|json|csv|md", "table|json|csv|md"))
	if err := fs.Parse(args); err != nil {
		return err
	}
	// The keyword may be positional, and flags may follow it:
	//   search --input app.apk oauth --kind class
	if rest := fs.Args(); len(rest) > 0 {
		if *keyword == "" {
			*keyword = rest[0]
		}
		if err := fs.Parse(rest[1:]); err != nil {
			return err
		}
	}
	kw := *keyword
	if kw == "" {
		return fmt.Errorf(i18n.T("--keyword is required", "需要 --keyword 参数"))
	}
	c, err := inputs.load()
	if err != nil {
		return err
	}
	// No SQLite catalog: the scan runs over the parsed model.
	res, err := store.Search(c, kw, kinds, *limit)
	if err != nil {
		return err
	}
	text, err := render(res, *format)
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

// cmdStats prints a one-screen summary straight from the parsed model, so it is
// fast even for inputs that take a while to materialise into SQLite.
func cmdStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	var inputs multiFlag
	addInputFlags(fs, &inputs)
	asJSON := fs.Bool("json", false, i18n.T("emit JSON", "输出 JSON"))
	top := fs.Int("top", 5, i18n.T("how many classes to list", "列出多少个类"))
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := inputs.load()
	if err != nil {
		return err
	}
	type classCount struct {
		Descriptor string `json:"descriptor"`
		Methods    int    `json:"methods"`
	}
	declared, calls := 0, 0
	perClass := map[string]int{}
	for _, f := range c.Files {
		for _, cd := range f.Classes {
			if cd.ClassData == nil {
				continue
			}
			n := len(cd.ClassData.DirectMethods) + len(cd.ClassData.VirtualMethods)
			declared += n
			perClass[cd.Descriptor] += n
		}
		for _, ci := range f.CodeItems {
			for _, ref := range ci.Refs {
				if ref.Kind == "method" {
					calls++
				}
			}
		}
	}
	topClasses := make([]classCount, 0, len(perClass))
	for k, v := range perClass {
		topClasses = append(topClasses, classCount{Descriptor: k, Methods: v})
	}
	sort.Slice(topClasses, func(i, j int) bool {
		if topClasses[i].Methods == topClasses[j].Methods {
			return topClasses[i].Descriptor < topClasses[j].Descriptor
		}
		return topClasses[i].Methods > topClasses[j].Methods
	})
	if *top > 0 && len(topClasses) > *top {
		topClasses = topClasses[:*top]
	}
	if *asJSON {
		out := map[string]any{
			"files":      c.Stats().Files,
			"dexBytes":   c.Stats().DexBytes,
			"strings":    c.Stats().Strings,
			"types":      c.Stats().Types,
			"classes":    c.Stats().Classes,
			"methods":    c.Stats().Methods,
			"fields":     c.Stats().Fields,
			"codeItems":  c.Stats().CodeItems,
			"declared":   declared,
			"callSites":  calls,
			"topClasses": topClasses,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	s := c.Stats()
	row := func(label string, value string) {
		fmt.Printf("%s %s\n", padDisplay(label, 12), value)
	}
	row(i18n.T("files", "文件"), fmt.Sprintf("%d", s.Files))
	row(i18n.T("dex bytes", "DEX 字节"), humanBytes(s.DexBytes))
	row(i18n.T("strings", "字符串"), comma(s.Strings))
	row(i18n.T("types", "类型"), comma(s.Types))
	row(i18n.T("classes", "类"), comma(s.Classes))
	row(i18n.T("methods", "方法"), fmt.Sprintf("%s (%s %s)", comma(s.Methods), i18n.T("declared", "其中声明"), comma(declared)))
	row(i18n.T("fields", "字段"), comma(s.Fields))
	row(i18n.T("code items", "代码项"), comma(s.CodeItems))
	row(i18n.T("call sites", "调用指令"), comma(calls))
	if len(topClasses) > 0 {
		fmt.Println(i18n.T("largest classes:", "最大的类:"))
		for i, cc := range topClasses {
			fmt.Printf("  %d. %-60s %s\n", i+1, cc.Descriptor, comma(cc.Methods))
		}
	}
	return nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func comma(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// cmdRecipes lists the built-in queries, or explains one in detail.
func cmdRecipes(args []string) error {
	fs := flag.NewFlagSet("recipes", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, i18n.T("emit JSON", "输出 JSON"))
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := fs.Arg(0)
	if *asJSON {
		list := store.Recipes
		if name != "" {
			r, ok := store.FindRecipe(name)
			if !ok {
				return fmt.Errorf(i18n.T("unknown recipe %q", "未知模板 %q"), name)
			}
			list = []store.Recipe{r}
		}
		b, _ := json.MarshalIndent(list, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if name != "" {
		r, ok := store.FindRecipe(name)
		if !ok {
			return fmt.Errorf(i18n.T("unknown recipe %q (run: dexsql-tree recipes)", "未知模板 %q（可运行 dexsql-tree recipes 查看）"), name)
		}
		fmt.Printf("%s - %s\n\n", r.Name, i18n.T(r.Doc, r.DocZH))
		for _, a := range r.Args {
			req := ""
			if a.Required {
				req = i18n.T(" (required)", "（必填）")
			}
			def := ""
			if a.Default != "" {
				def = fmt.Sprintf(i18n.T(" default %s", "，默认 %s"), a.Default)
			}
			fmt.Printf("  --arg %s=%s  %s%s%s\n", a.Name, a.Name, i18n.T(a.Doc, a.DocZH), req, def)
		}
		fmt.Println()
		fmt.Println(strings.TrimSpace(r.SQL))
		fmt.Println()
		fmt.Printf(i18n.T("run: dexsql-tree query --input app.apk --recipe %s", "运行: dexsql-tree query --input app.apk --recipe %s"), r.Name)
		for _, a := range r.Args {
			fmt.Printf(" --arg %s=...", a.Name)
		}
		fmt.Println()
		return nil
	}
	for _, r := range store.Recipes {
		fmt.Printf("%-14s %s\n", r.Name, i18n.T(r.Doc, r.DocZH))
	}
	return nil
}

func cmdTables(args []string) error {
	fs := flag.NewFlagSet("tables", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, i18n.T("emit JSON", "输出 JSON"))
	if err := fs.Parse(args); err != nil {
		return err
	}
	rels := store.AllRelations()
	if *asJSON {
		b, _ := json.MarshalIndent(rels, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	for _, t := range rels {
		suffix := ""
		if t.Kind == "view" {
			suffix = i18n.T(" [view]", " [视图]")
		}
		fmt.Printf("%-32s %s%s\n", t.Name, t.Doc, suffix)
	}
	return nil
}

func cmdTree(args []string) error {
	fs := flag.NewFlagSet("tree", flag.ContinueOnError)
	var inputs multiFlag
	addInputFlags(fs, &inputs)
	method := fs.String("method", "", i18n.T("root method descriptor", "根方法描述符"))
	direction := fs.String("direction", "callers", i18n.T("callers|callees", "callers|callees"))
	depth := fs.Int("depth", 3, i18n.T("maximum depth", "最大深度"))
	maxNodes := fs.Int("maxNodes", 5000, i18n.T("node budget", "节点上限"))
	format := fs.String("format", "text", i18n.T("text|json|dot", "text|json|dot"))
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *method == "" {
		return fmt.Errorf(i18n.T("--method is required", "需要 --method 参数"))
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
	addInputFlags(fs, &inputs)
	method := fs.String("method", "", i18n.T("method descriptor", "方法描述符"))
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *method == "" {
		return fmt.Errorf(i18n.T("--method is required", "需要 --method 参数"))
	}
	c, err := inputs.load()
	if err != nil {
		return err
	}
	f, idx, ok := c.FindMethod(*method)
	if !ok {
		return fmt.Errorf(i18n.T("method not found: %s", "未找到方法: %s"), *method)
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
	addInputFlags(fs, &inputs)
	addr := fs.String("addr", "127.0.0.1:8765", i18n.T("listen address", "监听地址"))
	token := fs.String("token", "", i18n.T(
		"require this bearer token on /api routes", "要求 /api 路由携带此 token"))
	disk := fs.Bool("disk", false, i18n.T(
		"use a temporary on-disk catalog to reduce memory", "使用临时文件目录以降低内存占用"))
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *token == "" && !loopbackAddr(*addr) {
		fmt.Fprintln(os.Stderr, i18n.T(
			"warning: listening on a non-loopback address without --token; anyone who can reach the port can query the loaded APKs",
			"警告: 正在监听非本机地址但没有 --token；能访问该端口的人都可以查询已加载的 APK"))
	}
	s := api.New()
	s.Debug = inputs.debug
	s.Disk = *disk
	s.Lazy = true
	s.Token = *token
	profile, err := store.ParseProfile(inputs.profile)
	if err != nil {
		return err
	}
	s.Profile = profile
	s.Cache = inputs.cache
	if inputs.cacheDir != "" {
		s.CacheDir = inputs.cacheDir
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		s.Close()
		os.Exit(0)
	}()
	if len(inputs.paths) > 0 {
		c, err := inputs.load()
		if err != nil {
			return err
		}
		opt, err := inputs.storeOptions(*disk)
		if err != nil {
			return err
		}
		// The first dataset is opened before any request arrives, so it has the
		// same lazy deal as the ones added over the API.
		opt.Lazy = s.Lazy
		st, err := store.Open(c, opt)
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
	addInputFlags(fs, &inputs)
	disk := fs.Bool("disk", false, i18n.T(
		"use a temporary on-disk catalog to reduce memory", "使用临时文件目录以降低内存占用"))
	if err := fs.Parse(args); err != nil {
		return err
	}
	srv := mcp.New(os.Stdout)
	srv.Debug = inputs.debug
	srv.Disk = *disk
	profile, err := store.ParseProfile(inputs.profile)
	if err != nil {
		return err
	}
	srv.Profile = profile
	srv.Cache = inputs.cache
	srv.CacheDir = inputs.cacheDir
	srv.Version = api.Version
	defer srv.Close()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		srv.Close()
		os.Exit(0)
	}()
	for _, p := range inputs.paths {
		if _, _, err := srv.Open(p); err != nil {
			return err
		}
	}
	return srv.Serve(os.Stdin)
}

// ---------------------------------------------------------------- language

const (
	blockStart = "# >>> dexsql-tree language >>>"
	blockEnd   = "# <<< dexsql-tree language <<<"
)

func cmdLang(args []string) error {
	fs := flag.NewFlagSet("lang", flag.ContinueOnError)
	write := fs.Bool("write", false, i18n.T(
		"append the setting to your shell profile", "把设置写入 shell 启动文件"))
	file := fs.String("file", "", i18n.T("profile file to edit (default: detected)", "要修改的启动文件（默认自动检测）"))
	shell := fs.String("shell", "", i18n.T("bash|zsh|fish|powershell|cmd", "bash|zsh|fish|powershell|cmd"))
	// Pull the action out of the positional list first so that
	// "lang env --shell fish" and "lang set zh --write" both work: the flag
	// package stops at the first non-flag argument.
	action, langValue := "", ""
	rest := make([]string, 0, len(args))
	valueFlags := map[string]bool{"--shell": true, "-shell": true, "--file": true, "-file": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			rest = append(rest, a)
			if valueFlags[a] && i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
			continue
		}
		switch {
		case action == "":
			action = a
		case langValue == "":
			langValue = a
		default:
			rest = append(rest, a)
		}
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if action == "" {
		action = "show"
	}
	sh := detectShell(*shell)
	path, pathErr := profilePath(sh)
	switch action {
	case "show", "status":
		// On/Source honour --lang as well as the environment.
		on, src := i18n.On(), i18n.Source()
		lang := "English"
		if on {
			lang = "中文"
		}
		row := func(label, value string) {
			fmt.Printf("%s %s\n", padDisplay(label, 12), value)
		}
		row(i18n.T("language:", "当前语言:"), fmt.Sprintf("%s (%s)", lang, i18n.Lang()))
		row(i18n.T("source:", "来源:"), src)
		row(i18n.T("enable:", "开启:"), assignment(sh, true))
		row(i18n.T("disable:", "关闭:"), assignment(sh, false))
		if pathErr == nil {
			row(i18n.T("profile:", "启动文件:"), path)
		}
		fmt.Println()
		fmt.Println(i18n.T(
			"Run 'dexsql-tree lang set zh --write' to make it permanent.",
			"运行 'dexsql-tree lang set zh --write' 可永久生效。"))
		return nil
	case "set", "on", "off", "unset", "clear":
		on := action == "set" || action == "on"
		if action == "set" {
			if langValue == "" {
				return fmt.Errorf(i18n.T("usage: dexsql-tree lang set zh|en [--write]", "用法: dexsql-tree lang set zh|en [--write]"))
			}
			var err error
			on, err = i18n.ParseFlagValue(langValue)
			if err != nil {
				return err
			}
		}
		remove := action == "unset" || action == "clear"
		if *write {
			if pathErr != nil {
				return pathErr
			}
			if *file != "" {
				path = *file
			}
			if sh == "cmd" {
				return fmt.Errorf(i18n.T(
					"cmd.exe has no profile; run this once: %s",
					"cmd.exe 没有启动脚本，请手动执行一次: %s"), assignment(sh, on))
			}
			if remove {
				changed, err := writeBlock(path, "", true)
				if err != nil {
					return err
				}
				if changed {
					fmt.Printf(i18n.T("removed the dexsql-tree block from %s\n",
						"已从 %s 移除 dexsql-tree 配置块\n"), path)
				} else {
					fmt.Printf(i18n.T("%s has no dexsql-tree block\n", "%s 中没有 dexsql-tree 配置块\n"), path)
				}
				return nil
			}
			line := assignment(sh, on)
			changed, err := writeBlock(path, line, false)
			if err != nil {
				return err
			}
			if changed {
				fmt.Printf(i18n.T("wrote %s to %s\n", "已写入 %s 到 %s\n"), line, path)
			} else {
				fmt.Printf(i18n.T("%s already contains %s\n", "%s 已包含 %s\n"), path, line)
			}
			fmt.Println(i18n.T(
				"Restart the shell or source the file to apply it.",
				"重新打开终端或 source 该文件后生效。"))
			return nil
		}
		if remove {
			fmt.Println(unsetLine(sh))
			return nil
		}
		fmt.Println(assignment(sh, on))
		fmt.Printf(i18n.T("# add --write to persist this in %s\n", "# 加上 --write 可写入 %s\n"), path)
		return nil
	case "env":
		// The assignment that turns Chinese on, ready for eval or copy-paste:
		//   eval "$(dexsql-tree lang env)"
		fmt.Println(assignment(sh, true))
		return nil
	case "path":
		if pathErr != nil {
			return pathErr
		}
		fmt.Println(path)
		return nil
	case "help", "list":
		fmt.Println(i18n.T(
			`usage: dexsql-tree lang [show|set zh|en|unset|env|path] [--write] [--shell NAME] [--file PATH]`,
			`用法: dexsql-tree lang [show|set zh|en|unset|env|path] [--write] [--shell 名称] [--file 路径]`))
		return nil
	default:
		return fmt.Errorf(i18n.T("unknown action %q", "未知操作 %q"), action)
	}
}

// assignment renders the shell-specific line that sets the environment
// variable, so users can copy it or let --write append it.
func assignment(shell string, on bool) string {
	value := "false"
	if on {
		value = "true"
	}
	switch shell {
	case "fish":
		if on {
			return "set -gx " + i18n.EnvZH + " true"
		}
		return "set -e " + i18n.EnvZH
	case "powershell", "pwsh":
		return fmt.Sprintf("$env:%s = %q", i18n.EnvZH, value)
	case "cmd":
		return "setx " + i18n.EnvZH + " " + value
	default:
		return "export " + i18n.EnvZH + "=" + value
	}
}

// unsetLine renders the shell-specific command that removes the variable for
// the current session.
func unsetLine(shell string) string {
	switch shell {
	case "fish":
		return "set -e " + i18n.EnvZH
	case "powershell", "pwsh":
		return "Remove-Item Env:" + i18n.EnvZH + " -ErrorAction SilentlyContinue"
	case "cmd":
		return "set " + i18n.EnvZH + "="
	default:
		return "unset " + i18n.EnvZH
	}
}

func detectShell(explicit string) string {
	if explicit != "" {
		return strings.ToLower(explicit)
	}
	if sh := filepath.Base(os.Getenv("SHELL")); sh != "" && sh != "." && sh != "/" {
		return strings.ToLower(sh)
	}
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "bash"
}

func profilePath(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc"), nil
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish"), nil
	case "bash":
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, ".bash_profile"), nil
		}
		return filepath.Join(home, ".bashrc"), nil
	case "powershell", "pwsh":
		if runtime.GOOS == "windows" {
			return filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1"), nil
		}
		return filepath.Join(home, ".config", "powershell", "Microsoft.PowerShell_profile.ps1"), nil
	case "cmd":
		return "", fmt.Errorf("cmd.exe has no profile file")
	default:
		return filepath.Join(home, ".profile"), nil
	}
}

// writeBlock appends the marked block to path, replacing any previous block.
func writeBlock(path, line string, remove bool) (bool, error) {
	mode := os.FileMode(0o644)
	var old []byte
	if b, err := os.ReadFile(path); err == nil {
		old = b
		if fi, statErr := os.Stat(path); statErr == nil {
			mode = fi.Mode().Perm()
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	clean := strings.TrimRight(stripBlock(string(old)), "\n")
	next := clean
	if !remove {
		if next != "" {
			next += "\n\n"
		}
		next += blockStart + "\n" + line + "\n" + blockEnd + "\n"
	} else if next != "" {
		next += "\n"
	}
	if next == string(old) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(next), mode); err != nil {
		return false, err
	}
	return true, nil
}

// stripBlock removes our marker region, keeping every other line untouched.
func stripBlock(text string) string {
	if !strings.Contains(text, blockStart) {
		return text
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		switch {
		case trim == blockStart:
			skip = true
			continue
		case trim == blockEnd:
			skip = false
			continue
		}
		if !skip {
			out = append(out, line)
		}
	}
	res := strings.Join(out, "\n")
	// Collapse the blank line that separated the block from the rest.
	for strings.Contains(res, "\n\n\n") {
		res = strings.ReplaceAll(res, "\n\n\n", "\n\n")
	}
	return res
}

// ---------------------------------------------------------------- cache

// cmdCache manages the persistent catalog cache: list, clear, path, info.
func cmdCache(args []string) error {
	action := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("cache", flag.ContinueOnError)
	dirFlag := fs.String("dir", "", i18n.T("cache directory", "缓存目录"))
	var inputs multiFlag
	addInputFlags(fs, &inputs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := *dirFlag
	if dir == "" {
		dir = cache.Dir()
	}
	switch action {
	case "list", "ls":
		ents, err := cache.List(dir)
		if err != nil {
			return err
		}
		if len(ents) == 0 {
			fmt.Printf(i18n.T("cache is empty: %s\n", "缓存为空: %s\n"), dir)
			return nil
		}
		var total int64
		w := 0
		for _, e := range ents {
			total += e.Size
			if n := len(e.Key); n > w {
				w = n
			}
		}
		fmt.Printf("%-*s  %10s  %s\n", w, i18n.T("key", "键"), i18n.T("size", "大小"), i18n.T("modified", "修改时间"))
		for _, e := range ents {
			fmt.Printf("%-*s  %10s  %s\n", w, e.Key, humanBytes(e.Size), e.ModTime.Format("2006-01-02 15:04"))
		}
		fmt.Printf(i18n.T("\n%d catalogs, %s total, in %s\n", "\n共 %d 个目录，合计 %s，位置 %s\n"),
			len(ents), humanBytes(total), dir)
	case "clear", "clean", "rm":
		n, b, err := cache.Clear(dir)
		if err != nil {
			return err
		}
		fmt.Printf(i18n.T("removed %d catalogs (%s) from %s\n", "已删除 %d 个目录（%s），位置 %s\n"), n, humanBytes(b), dir)
	case "prune":
		// The key already includes the version, so an old catalog is never
		// reused — but it also never disappears on its own, and one 0.3.0
		// catalog for an 11 MB APK is half a gigabyte.
		ents, err := cache.List(dir)
		if err != nil {
			return err
		}
		var removed, kept int
		var freed int64
		for _, e := range ents {
			v, verr := store.CatalogVersion(e.Path)
			if verr == nil && v == api.Version {
				kept++
				continue
			}
			if err := os.Remove(e.Path); err != nil {
				continue
			}
			removed++
			freed += e.Size
		}
		if removed == 0 {
			fmt.Printf(i18n.T("nothing to prune: %d catalogs match %s\n",
				"没有可清理的目录：%d 个与 %s 匹配\n"), kept, api.Version)
			return nil
		}
		fmt.Printf(i18n.T("pruned %d stale catalogs (%s), kept %d\n",
			"已清理 %d 个旧目录（%s），保留 %d 个\n"), removed, humanBytes(freed), kept)
	case "path", "dir":
		fmt.Println(dir)
	case "info":
		if len(inputs.paths) == 0 {
			return fmt.Errorf(i18n.T("cache info needs --input", "cache info 需要 --input 参数"))
		}
		profile, err := store.ParseProfile(inputs.profile)
		if err != nil {
			return err
		}
		key := cache.Key(inputs.paths, string(profile), api.Version, inputs.debug)
		ents, err := cache.ForInput(dir, key)
		if err != nil {
			return err
		}
		if len(ents) == 0 {
			fmt.Printf(i18n.T("no catalog for this input (key %s); run a query with --cache to build it\n",
				"该输入还没有目录（键 %s）；带 --cache 运行一次查询即可生成\n"), key)
			return nil
		}
		var total int64
		newest := ents[0].ModTime
		full := false
		for _, e := range ents {
			total += e.Size
			if e.ModTime.After(newest) {
				newest = e.ModTime
			}
			if e.Key == key {
				full = true
			}
		}
		fmt.Printf("%s %s\n", padDisplay(i18n.T("key:", "键:"), 12), key)
		if len(ents) == 1 && full {
			fmt.Printf("%s %s\n", padDisplay(i18n.T("path:", "路径:"), 12), ents[0].Path)
		} else {
			kind := i18n.T("on demand", "按需")
			if full {
				kind = i18n.T("full + on demand", "完整 + 按需")
			}
			fmt.Printf("%s %d (%s)\n", padDisplay(i18n.T("catalogs:", "目录数:"), 12), len(ents), kind)
		}
		fmt.Printf("%s %s\n", padDisplay(i18n.T("size:", "大小:"), 12), humanBytes(total))
		fmt.Printf("%s %s\n", padDisplay(i18n.T("built:", "生成于:"), 12), newest.Format("2006-01-02 15:04:05"))
		fmt.Printf("%s %s\n", padDisplay(i18n.T("profile:", "档位:"), 12), profile)
	default:
		return fmt.Errorf(i18n.T("unknown cache action %q (list|clear|prune|path|info)", "未知的 cache 操作 %q（list|clear|prune|path|info）"), action)
	}
	return nil
}
