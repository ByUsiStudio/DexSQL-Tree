package dex_test

import (
	"strings"
	"testing"

	"github.com/byusi-studio/dexsql-tree/internal/dex"
	"github.com/byusi-studio/dexsql-tree/internal/dextest"
)

func TestParseHello(t *testing.T) {
	f, err := dex.Parse(dextest.HelloDex(), "hello.dex", "classes.dex", 0, dex.Options{Debug: true})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.Header.Version != "035" || f.Header.ClassCount != 1 {
		t.Fatalf("unexpected header: %+v", f.Header)
	}
	if len(f.Strings) == 0 || len(f.Methods) != 2 {
		t.Fatalf("strings=%d methods=%d", len(f.Strings), len(f.Methods))
	}
	main := f.Methods[0].Descriptor
	if main != "Ldemo/Hello;->main([Ljava/lang/String;)V" {
		t.Fatalf("method descriptor = %q", main)
	}
	ci := f.CodeItems[0]
	if ci == nil {
		t.Fatal("no code item for main")
	}
	if ci.OpcodeCount != 4 {
		t.Fatalf("opcode_count=%d want 4", ci.OpcodeCount)
	}
	if len(ci.Refs) != 3 {
		t.Fatalf("refs=%d want 3 (%+v)", len(ci.Refs), ci.Refs)
	}
	if len(ci.Tries) != 0 {
		t.Fatalf("unexpected tries")
	}
	smali, err := f.Smali(0)
	if err != nil {
		t.Fatalf("smali: %v", err)
	}
	for _, want := range []string{"const-string v0", "sget-object v1", "invoke-virtual {v1, v0}", "return-void"} {
		if !strings.Contains(smali, want) {
			t.Errorf("smali missing %q:\n%s", want, smali)
		}
	}
	if got := f.Classes[0].SuperDescriptor; got != "Ljava/lang/Object;" {
		t.Errorf("superclass = %q", got)
	}
	if got := f.Classes[0].SourceFile; got != "Hello.java" {
		t.Errorf("source file = %q", got)
	}
	if len(f.Positions[0]) != 4 {
		t.Errorf("positions = %d want 4", len(f.Positions[0]))
	}
}

func TestJavaName(t *testing.T) {
	cases := map[string]string{
		"Ljava/lang/String;":   "java.lang.String",
		"[I":                   "int[]",
		"[[Ljava/lang/Object;": "java.lang.Object[][]",
		"V":                    "void",
		"I":                    "int",
	}
	for in, want := range cases {
		if got := dex.JavaName(in); got != want {
			t.Errorf("JavaName(%q) = %q want %q", in, got, want)
		}
	}
}

func TestDecodeMUTF8(t *testing.T) {
	// "A\0B" encoded as MUTF-8, plus a surrogate pair for U+1F600.
	in := []byte{'A', 0xc0, 0x80, 'B', 0xed, 0xa0, 0xbd, 0xed, 0xb8, 0x80}
	if got := dex.MUTF8String(in); got != "A\x00B\U0001F600" {
		t.Errorf("MUTF8String = %q", got)
	}
}

func TestSmaliOperandOrder(t *testing.T) {
	f, err := dex.Parse(dextest.OpsDex(), "ops.dex", "classes.dex", 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	text, err := f.Smali(0)
	if err != nil {
		t.Fatalf("smali: %v", err)
	}
	for _, want := range []string{
		"const/4 v0, #1",
		"if-eq v0, v1, :L5",
		"iput v0, v1, Ldemo/Ops;->f:I",
		"const/4 v1, #-1",
		"add-int/lit16 v0, v0, #4",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestDebugPositionsAreOptIn(t *testing.T) {
	// Default: headers are kept, the expensive line program is skipped.
	f, err := dex.Parse(dextest.HelloDex(), "hello.dex", "classes.dex", 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Positions) != 0 {
		t.Fatalf("positions parsed without Debug: %d methods", len(f.Positions))
	}
	if len(f.DebugInfos) != 1 {
		t.Fatalf("debug_info headers = %d, want 1", len(f.DebugInfos))
	}

	g, err := dex.Parse(dextest.HelloDex(), "hello.dex", "classes.dex", 0, dex.Options{Debug: true})
	if err != nil {
		t.Fatalf("parse with Debug: %v", err)
	}
	if got := len(g.Positions[0]); got != 4 {
		t.Fatalf("positions = %d, want 4", got)
	}
}
