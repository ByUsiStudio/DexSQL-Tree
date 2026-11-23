package tree_test

import (
	"strings"
	"testing"

	"github.com/byusi-studio/dexsql-tree/internal/dex"
	"github.com/byusi-studio/dexsql-tree/internal/dextest"
	"github.com/byusi-studio/dexsql-tree/internal/tree"
)

func graph(t *testing.T) *tree.Graph {
	t.Helper()
	c, err := dex.ParseContainer(dextest.HelloDex(), "hello.dex")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree.BuildGraph(c)
}

func TestCallers(t *testing.T) {
	g := graph(t)
	res, err := g.Build("Ljava/io/PrintStream;->println(Ljava/lang/String;)V", tree.Options{Direction: "callers", Depth: 3})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(res.Root.Children) != 1 {
		t.Fatalf("children = %d", len(res.Root.Children))
	}
	if got := res.Root.Children[0].Descriptor; got != "Ldemo/Hello;->main([Ljava/lang/String;)V" {
		t.Fatalf("caller = %q", got)
	}
	if res.NodeCount != 2 {
		t.Fatalf("node count = %d", res.NodeCount)
	}
	if !strings.Contains(res.Text(), "callers of") {
		t.Fatalf("text missing header:\n%s", res.Text())
	}
}

func TestCalleesDepthLimit(t *testing.T) {
	g := graph(t)
	res, err := g.Build("Ldemo/Hello;->main([Ljava/lang/String;)V", tree.Options{Direction: "callees", Depth: 1})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(res.Root.Children) != 1 || res.Root.Children[0].Depth != 1 {
		t.Fatalf("unexpected tree: %+v", res.Root)
	}
	if res.Root.Children[0].Children != nil {
		t.Fatalf("depth limit not applied")
	}
}

func TestUnknownMethod(t *testing.T) {
	g := graph(t)
	if _, err := g.Build("Lx;->y()V", tree.Options{}); err == nil {
		t.Fatal("expected error for unknown method")
	}
}

func TestCycleAndBudget(t *testing.T) {
	node := func(d string) tree.Node {
		return tree.Node{Descriptor: d, Class: "La;", Name: "m"}
	}
	g := &tree.Graph{
		Nodes: map[string]tree.Node{"La;->a()V": node("La;->a()V"), "La;->b()V": node("La;->b()V")},
		Out: map[string][]tree.Edge{
			"La;->a()V": {{From: "La;->a()V", To: "La;->b()V", Count: 1}},
			"La;->b()V": {{From: "La;->b()V", To: "La;->a()V", Count: 1}},
		},
		In: map[string][]tree.Edge{
			"La;->a()V": {{From: "La;->b()V", To: "La;->a()V", Count: 1}},
			"La;->b()V": {{From: "La;->a()V", To: "La;->b()V", Count: 1}},
		},
	}
	res, err := g.Build("La;->a()V", tree.Options{Direction: "callees", Depth: 6})
	if err != nil {
		t.Fatal(err)
	}
	child := res.Root.Children[0]
	if len(child.Children) != 1 || !child.Children[0].Cycle {
		t.Fatalf("cycle not detected: %+v", child.Children)
	}
	res2, err := g.Build("La;->a()V", tree.Options{Direction: "callees", Depth: 6, MaxNodes: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Truncated {
		t.Fatalf("node budget not applied")
	}
}
