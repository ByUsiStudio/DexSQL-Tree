package store

import (
	"fmt"
	"sort"
	"strings"
)

// A Recipe is a named, parameterised SELECT that answers a question people
// usually get wrong the first time: "who calls this method", "which strings
// look like URLs", "how big are the biggest methods". Recipes keep their SQL in
// one place so the CLI, the HTTP API and the MCP server all give the same
// answer, and so nobody has to remember the join shape.
type Recipe struct {
	Name  string      `json:"name"`
	Doc   string      `json:"doc"`
	DocZH string      `json:"docZh"`
	SQL   string      `json:"sql"`
	Args  []RecipeArg `json:"args,omitempty"`
}

// RecipeArg describes one :placeholder of a recipe.
type RecipeArg struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Default  string `json:"default,omitempty"`
	Doc      string `json:"doc"`
	DocZH    string `json:"docZh"`
}

// Recipes is the built-in recipe book.
var Recipes = []Recipe{
	{
		Name:  "callers",
		Doc:   "Who calls this method, aggregated by call site count.",
		DocZH: "谁调用了这个方法，按调用次数聚合。",
		SQL: `SELECT method AS caller, method_class, method_name, COUNT(*) AS calls
FROM method_calls
WHERE callee = :method
GROUP BY method, method_class, method_name
ORDER BY calls DESC, caller`,
		Args: []RecipeArg{{Name: "method", Required: true,
			Doc:   "Full callee descriptor, e.g. Ljava/io/PrintStream;->println(Ljava/lang/String;)V",
			DocZH: "被调用方法的完整描述符，例如 Ljava/io/PrintStream;->println(Ljava/lang/String;)V"}},
	},
	{
		Name:  "callees",
		Doc:   "What this method calls, aggregated by call site count.",
		DocZH: "这个方法调用了谁，按调用次数聚合。",
		SQL: `SELECT callee, callee_class, callee_name, COUNT(*) AS calls
FROM method_calls
WHERE method = :method
GROUP BY callee, callee_class, callee_name
ORDER BY calls DESC, callee`,
		Args: []RecipeArg{{Name: "method", Required: true,
			Doc: "Full caller descriptor.", DocZH: "调用方方法的完整描述符。"}},
	},
	{
		Name:  "methods-like",
		Doc:   "Find methods whose descriptor matches a LIKE pattern, with their Java class.",
		DocZH: "按 LIKE 模式查找方法，并给出对应的 Java 类名。",
		SQL: `SELECT java_class, name, descriptor, opcode_count, is_public, is_static
FROM v_methods
WHERE descriptor LIKE :pattern
ORDER BY descriptor`,
		Args: []RecipeArg{{Name: "pattern", Default: "%", Required: true,
			Doc: "LIKE pattern, e.g. %password%", DocZH: "LIKE 模式，例如 %password%"}},
	},
	{
		Name:  "strings-like",
		Doc:   "Find string constants by LIKE pattern, ranked by how many instructions use them.",
		DocZH: "按 LIKE 模式查找字符串常量，按被引用次数排序。",
		SQL: `SELECT value, COUNT(*) AS uses
FROM method_strings
WHERE value LIKE :pattern
GROUP BY value
ORDER BY uses DESC, value`,
		Args: []RecipeArg{{Name: "pattern", Required: true,
			Doc: "LIKE pattern, e.g. %api%", DocZH: "LIKE 模式，例如 %api%"}},
	},
	{
		Name:  "api-usage",
		Doc:   "Count call sites per API, ranked by usage; the quickest way to spot SDK and network calls.",
		DocZH: "统计每个 API 的调用点数量并排序，快速定位 SDK 与网络调用。",
		SQL: `SELECT callee_class, callee_name, COUNT(*) AS calls, COUNT(DISTINCT method) AS callers
FROM method_calls
WHERE callee LIKE :pattern
GROUP BY callee_class, callee_name
ORDER BY calls DESC, callee_class`,
		Args: []RecipeArg{{Name: "pattern", Default: "%", Required: true,
			Doc: "LIKE pattern over the callee descriptor.", DocZH: "作用于被调方描述符的 LIKE 模式。"}},
	},
	{
		Name:  "class-info",
		Doc:   "One class with its superclass, source file and declared member counts.",
		DocZH: "单个类的父类、源文件以及声明的成员数量。",
		SQL: `SELECT c.descriptor, c.superclass_descriptor, c.access_flags, c.source_file,
       (SELECT COUNT(*) FROM class_methods m
         WHERE m.class_descriptor = c.descriptor) AS methods,
       (SELECT COUNT(*) FROM class_fields f
         WHERE f.class_descriptor = c.descriptor) AS fields
FROM classes c
WHERE c.descriptor LIKE :class
ORDER BY c.descriptor`,
		Args: []RecipeArg{{Name: "class", Required: true,
			Doc:   "Class descriptor or LIKE pattern, e.g. Lcom/example/%",
			DocZH: "类描述符或 LIKE 模式，例如 Lcom/example/%"}},
	},
	{
		Name:  "big-methods",
		Doc:   "Largest methods by instruction count.",
		DocZH: "按指令数量排序的最大方法。",
		SQL: `SELECT java_class, name, descriptor, opcode_count
FROM v_methods
WHERE opcode_count IS NOT NULL
ORDER BY opcode_count DESC, descriptor
LIMIT 20`,
	},
	{
		Name:  "urls",
		Doc:   "String constants that look like endpoints or credentials, ranked by usage.",
		DocZH: "看起来像接口地址或凭据的字符串常量，按引用次数排序。",
		SQL: `SELECT value, COUNT(*) AS uses
FROM method_strings
WHERE value LIKE '%http://%' OR value LIKE '%https://%'
   OR value LIKE '%api_key%' OR value LIKE '%apikey%'
   OR value LIKE '%password%' OR value LIKE '%secret%'
GROUP BY value
ORDER BY uses DESC, value
LIMIT 50`,
	},
}

// FindRecipe returns the recipe called name.
func FindRecipe(name string) (Recipe, bool) {
	for _, r := range Recipes {
		if r.Name == name {
			return r, true
		}
	}
	return Recipe{}, false
}

// Render substitutes :placeholders with quoted literals and validates that
// required arguments are present.
func (r Recipe) Render(args map[string]string) (string, error) {
	values := map[string]string{}
	for _, a := range r.Args {
		v, ok := args[a.Name]
		if !ok || strings.TrimSpace(v) == "" {
			v = a.Default
		}
		if v == "" && a.Required {
			return "", fmt.Errorf("recipe %s needs --arg %s (%s)", r.Name, a.Name, a.Doc)
		}
		values[a.Name] = v
	}
	// Longest name first so :class does not shadow :class_descriptor.
	names := make([]string, 0, len(values))
	for k := range values {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	sql := r.SQL
	for _, name := range names {
		sql = strings.ReplaceAll(sql, ":"+name, sqlQuote(values[name]))
	}
	return sql, nil
}

// sqlQuote renders a Go string as a SQLite text literal.
func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
