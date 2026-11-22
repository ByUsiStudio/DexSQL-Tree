// Package tree builds caller/callee trees over a loaded container. This is the
// "tree" half of dexsql-tree: SQL answers flat questions, the tree view answers
// reachability questions with cycle detection and depth limits.
package tree

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dexsql-tree/dexsql-tree/internal/dex"
)

// Edge is one aggregated caller -> callee relationship.
type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Count int    `json:"count"`
}

// Node is one method in the returned tree.
type Node struct {
	Descriptor string  `json:"descriptor"`
	Class      string  `json:"class"`
	Name       string  `json:"name"`
	FileID     uint32  `json:"fileId"`
	MethodIdx  uint32  `json:"methodIdx"`
	Depth      int     `json:"depth"`
	Calls      int     `json:"calls"`
	Cycle      bool    `json:"cycle,omitempty"`
	Children   []*Node `json:"children,omitempty"`
}

// Graph is an aggregated call graph for a container.
type Graph struct {
	Nodes map[string]Node
	Out   map[string][]Edge
	In    map[string][]Edge
}

// BuildGraph indexes every call instruction in the container.
func BuildGraph(c *dex.Container) *Graph {
	g := &Graph{Nodes: map[string]Node{}, Out: map[string][]Edge{}, In: map[string][]Edge{}}
	type key struct{ from, to string }
	counts := map[key]int{}
	for _, f := range c.Files {
		// Seed every method so leaf methods (no edges) are still valid roots.
		for mi := range f.Methods {
			m := f.Methods[mi]
			if _, ok := g.Nodes[m.Descriptor]; !ok {
				g.Nodes[m.Descriptor] = Node{
					Descriptor: m.Descriptor, Class: m.ClassDesc, Name: m.Name,
					FileID: f.FileID, MethodIdx: uint32(mi),
				}
			}
		}
		local := map[key]int{}
		for mi := range f.Methods {
			ci := f.CodeItems[uint32(mi)]
			if ci == nil {
				continue
			}
			from := f.Methods[mi]
			for _, ref := range ci.Refs {
				if ref.Kind != "method" || int(ref.Index) >= len(f.Methods) {
					continue
				}
				to := f.Methods[ref.Index]
				k := key{from.Descriptor, to.Descriptor}
				local[k]++
				if _, ok := g.Nodes[from.Descriptor]; !ok {
					g.Nodes[from.Descriptor] = Node{
						Descriptor: from.Descriptor, Class: from.ClassDesc, Name: from.Name,
						FileID: f.FileID, MethodIdx: uint32(mi),
					}
				}
				if _, ok := g.Nodes[to.Descriptor]; !ok {
					g.Nodes[to.Descriptor] = Node{
						Descriptor: to.Descriptor, Class: to.ClassDesc, Name: to.Name,
						FileID: f.FileID, MethodIdx: ref.Index,
					}
				}
			}
		}
		for k, v := range local {
			counts[k] += v
		}
	}
	for k, v := range counts {
		g.Out[k.from] = append(g.Out[k.from], Edge{From: k.from, To: k.to, Count: v})
		g.In[k.to] = append(g.In[k.to], Edge{From: k.from, To: k.to, Count: v})
	}
	for k := range g.Out {
		sort.Slice(g.Out[k], func(i, j int) bool {
			if g.Out[k][i].Count == g.Out[k][j].Count {
				return g.Out[k][i].To < g.Out[k][j].To
			}
			return g.Out[k][i].Count > g.Out[k][j].Count
		})
	}
	for k := range g.In {
		sort.Slice(g.In[k], func(i, j int) bool {
			if g.In[k][i].Count == g.In[k][j].Count {
				return g.In[k][i].From < g.In[k][j].From
			}
			return g.In[k][i].Count > g.In[k][j].Count
		})
	}
	return g
}

// Options controls tree construction.
type Options struct {
	Direction string // "callers" or "callees"
	Depth     int
	MaxNodes  int
}

// Result is a tree plus traversal statistics.
type Result struct {
	Root      *Node  `json:"root"`
	Direction string `json:"direction"`
	MaxDepth  int    `json:"maxDepth"`
	NodeCount int    `json:"nodeCount"`
	EdgeCount int    `json:"edgeCount"`
	Truncated bool   `json:"truncated"`
}

// Build returns the call tree rooted at descriptor.
func (g *Graph) Build(descriptor string, opts Options) (*Result, error) {
	if opts.Direction == "" {
		opts.Direction = "callers"
	}
	if opts.Direction != "callers" && opts.Direction != "callees" {
		return nil, fmt.Errorf("direction must be callers or callees")
	}
	if opts.Depth <= 0 {
		opts.Depth = 3
	}
	if opts.Depth > 32 {
		opts.Depth = 32
	}
	if opts.MaxNodes <= 0 {
		opts.MaxNodes = 5000
	}
	rootInfo, ok := g.Nodes[descriptor]
	if !ok {
		// The method may exist but never participate in a call edge.
		if _, ok2 := g.Out[descriptor]; !ok2 {
			return nil, fmt.Errorf("method not found in call graph: %s", descriptor)
		}
	}
	if rootInfo.Descriptor == "" {
		rootInfo = Node{Descriptor: descriptor, Class: classOf(descriptor), Name: nameOf(descriptor)}
	}
	res := &Result{Root: &Node{
		Descriptor: descriptor, Class: rootInfo.Class, Name: rootInfo.Name,
		FileID: rootInfo.FileID, MethodIdx: rootInfo.MethodIdx, Depth: 0,
	}, Direction: opts.Direction, MaxDepth: opts.Depth, NodeCount: 1}

	var walk func(cur *Node, depth int, path map[string]bool)
	walk = func(cur *Node, depth int, path map[string]bool) {
		if depth >= opts.Depth || res.Truncated {
			return
		}
		var edges []Edge
		if opts.Direction == "callers" {
			edges = g.In[cur.Descriptor]
		} else {
			edges = g.Out[cur.Descriptor]
		}
		for _, e := range edges {
			next := e.To
			if opts.Direction == "callers" {
				next = e.From
			}
			info := g.Nodes[next]
			if info.Descriptor == "" {
				info = Node{Descriptor: next, Class: classOf(next), Name: nameOf(next)}
			}
			n := &Node{
				Descriptor: next, Class: info.Class, Name: info.Name,
				FileID: info.FileID, MethodIdx: info.MethodIdx, Depth: depth + 1, Calls: e.Count,
			}
			if res.NodeCount >= opts.MaxNodes {
				res.Truncated = true
				return
			}
			if path[next] {
				n.Cycle = true
				cur.Children = append(cur.Children, n)
				res.NodeCount++
				res.EdgeCount++
				continue
			}
			cur.Children = append(cur.Children, n)
			res.NodeCount++
			res.EdgeCount++
			path[next] = true
			walk(n, depth+1, path)
			delete(path, next)
		}
	}
	path := map[string]bool{descriptor: true}
	walk(res.Root, 0, path)
	return res, nil
}

func classOf(desc string) string {
	if i := strings.Index(desc, "->"); i > 0 {
		return desc[:i]
	}
	return ""
}

func nameOf(desc string) string {
	i := strings.Index(desc, "->")
	if i < 0 {
		return desc
	}
	rest := desc[i+2:]
	if j := strings.IndexByte(rest, '('); j >= 0 {
		return rest[:j]
	}
	return rest
}

// Text renders the tree as an indented list.
func (r *Result) Text() string {
	var b strings.Builder
	direction := "callers of"
	if r.Direction == "callees" {
		direction = "callees of"
	}
	fmt.Fprintf(&b, "%s %s (depth %d, %d nodes)\n", direction, r.Root.Descriptor, r.MaxDepth, r.NodeCount)
	var rec func(n *Node, prefix string, last bool, root bool)
	rec = func(n *Node, prefix string, last bool, root bool) {
		if root {
			fmt.Fprintf(&b, "%s\n", n.Descriptor)
		} else {
			branch := "├─ "
			next := prefix + "│  "
			if last {
				branch = "└─ "
				next = prefix + "   "
			}
			suffix := ""
			if n.Calls > 1 {
				suffix = fmt.Sprintf("  (x%d)", n.Calls)
			}
			if n.Cycle {
				suffix += "  [cycle]"
			}
			fmt.Fprintf(&b, "%s%s%s%s\n", prefix, branch, n.Descriptor, suffix)
			prefix = next
		}
		for i, c := range n.Children {
			rec(c, prefix, i == len(n.Children)-1, false)
		}
	}
	rec(r.Root, "", true, true)
	if r.Truncated {
		b.WriteString("... truncated\n")
	}
	return b.String()
}

// DOT renders a Graphviz digraph of the tree.
func (r *Result) DOT() string {
	var b strings.Builder
	b.WriteString("digraph dexsql_tree {\n  rankdir=LR;\n  node [shape=box, fontsize=10];\n")
	var rec func(n *Node, id int) int
	next := 0
	rec = func(n *Node, parent int) int {
		id := next
		next++
		label := strings.ReplaceAll(n.Name, "\"", "\\\"")
		style := ""
		if n.Cycle {
			style = ", style=dashed, color=gray"
		}
		fmt.Fprintf(&b, "  n%d [label=\"%s\"%s];\n", id, label, style)
		if parent >= 0 {
			fmt.Fprintf(&b, "  n%d -> n%d;\n", parent, id)
		}
		for _, c := range n.Children {
			rec(c, id)
		}
		return id
	}
	rec(r.Root, -1)
	b.WriteString("}\n")
	return b.String()
}
