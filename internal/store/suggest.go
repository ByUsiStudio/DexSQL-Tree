package store

import (
	"fmt"
	"strings"
)

// Suggest turns a raw SQLite error into something a first-time user can act on.
// The engine's job is not to be a SQL tutor, but "no such table: method_call"
// with no follow-up is where a newcomer stops.
func Suggest(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "no such table"):
		name := lastToken(msg)
		if near := nearest(name, relationNames()); near != "" {
			return fmt.Errorf("%w\ndid you mean %q? run `dexsql-tree tables` for the full list", err, near)
		}
		return fmt.Errorf("%w\nrun `dexsql-tree tables` for the list of relations", err)

	case strings.Contains(lower, "no such column"):
		name := lastToken(msg)
		if near := nearest(name, allColumnNames()); near != "" {
			return fmt.Errorf("%w\ndid you mean %q? run `dexsql-tree tables <relation>` to see its columns", err, near)
		}
		return fmt.Errorf("%w\nrun `dexsql-tree tables <relation>` to see its columns", err)

	case strings.Contains(lower, "syntax error"):
		return fmt.Errorf("%w\ncheck that string literals are closed and identifiers with spaces are quoted", err)
	}
	return err
}

// lastToken returns the identifier SQLite appends after the colon. The pure-Go
// driver adds an extended result code ("no such table: x (1)"), so the token
// ends at the first space or parenthesis.
func lastToken(msg string) string {
	i := strings.LastIndex(msg, ":")
	if i < 0 || i+1 >= len(msg) {
		return ""
	}
	s := strings.TrimSpace(msg[i+1:])
	if j := strings.IndexAny(s, " ("); j > 0 {
		s = s[:j]
	}
	return s
}

func relationNames() []string {
	rels := AllRelations()
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, r.Name)
	}
	return out
}

func allColumnNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range AllRelations() {
		for _, c := range r.Columns() {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// nearest returns the closest candidate within a small edit distance, or "".
// Names in this schema differ by a letter or two (method_calls/method_refs),
// which is exactly the mistake a typo produces.
func nearest(name string, candidates []string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	best, bestDist := "", 3
	for _, c := range candidates {
		if d := editDistance(name, strings.ToLower(c)); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

// editDistance is Levenshtein with a two-row buffer; names are short.
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
