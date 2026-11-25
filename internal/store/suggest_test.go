package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/byusi-studio/dexsql-tree/internal/store"
)

// A message that stops at "no such table" is where a newcomer gives up; these
// assertions pin the follow-up advice, not the exact wording.
func TestSuggest(t *testing.T) {
	cases := []struct{ in, want string }{
		{"SQL logic error: no such table: method_call (1)", "method_calls"},
		{"SQL logic error: no such column: namee (1)", `"name"`},
		{`near "selec": syntax error`, "string literals are closed"},
	}
	for _, c := range cases {
		got := store.Suggest(errors.New(c.in))
		if got == nil || !strings.Contains(got.Error(), c.want) {
			t.Errorf("Suggest(%q) = %v, want it to mention %q", c.in, got, c.want)
		}
	}
	// An unrelated error must pass through untouched.
	plain := errors.New("context deadline exceeded")
	if got := store.Suggest(plain); got.Error() != plain.Error() {
		t.Errorf("unrelated error was rewritten: %v", got)
	}
	if store.Suggest(nil) != nil {
		t.Errorf("nil should stay nil")
	}
}
