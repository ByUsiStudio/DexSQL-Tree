package i18n

import "testing"

func reset() {
	mu.Lock()
	resolved = false
	zh = false
	source = "default"
	mu.Unlock()
}

func TestDefaultsToEnglish(t *testing.T) {
	reset()
	t.Setenv(EnvZH, "")
	t.Setenv(EnvZHAlias, "")
	t.Setenv(EnvLang, "")
	if On() {
		t.Fatal("expected English by default")
	}
	if got := T("rows", "行"); got != "rows" {
		t.Fatalf("T = %q", got)
	}
}

func TestTruthySpellings(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "1", "yes", "on", "zh", "zh-CN", "中文"} {
		reset()
		t.Setenv(EnvZH, v)
		if !On() {
			t.Errorf("%s=%q should enable Chinese", EnvZH, v)
		}
		if Source() != EnvZH {
			t.Errorf("source = %q", Source())
		}
	}
	for _, v := range []string{"false", "0", "no", "off", "en", "en_US"} {
		reset()
		t.Setenv(EnvZH, v)
		if On() {
			t.Errorf("%s=%q should keep English", EnvZH, v)
		}
	}
}

func TestHyphenatedAlias(t *testing.T) {
	reset()
	t.Setenv(EnvZH, "")
	t.Setenv(EnvZHAlias, "true")
	if !On() {
		t.Fatal("hyphenated alias should be read")
	}
	if Source() != EnvZHAlias {
		t.Fatalf("source = %q", Source())
	}
}

func TestLangVariableWins(t *testing.T) {
	reset()
	t.Setenv(EnvZH, "true")
	t.Setenv(EnvLang, "en")
	if On() {
		t.Fatal("DEXSQL_TREE_LANG=en should win over DEXSQL_TREE_ZH=true")
	}
}

func TestSetOverridesEnvironment(t *testing.T) {
	reset()
	t.Setenv(EnvZH, "false")
	Set(true)
	if !On() || Source() != "flag" {
		t.Fatalf("Set(true) ignored: on=%v source=%q", On(), Source())
	}
	reset()
}

func TestConsumeLangFlag(t *testing.T) {
	rest, err := ConsumeLangFlag([]string{"query", "--lang", "zh", "--sql", "SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	if !On() {
		t.Fatal("--lang zh should enable Chinese")
	}
	if len(rest) != 3 || rest[0] != "query" || rest[1] != "--sql" {
		t.Fatalf("rest = %v", rest)
	}
	reset()
	rest, err = ConsumeLangFlag([]string{"--lang=en", "search"})
	if err != nil || On() || len(rest) != 1 {
		t.Fatalf("rest=%v err=%v on=%v", rest, err, On())
	}
	if _, err := ConsumeLangFlag([]string{"--lang"}); err == nil {
		t.Fatal("missing value should fail")
	}
	if _, err := ConsumeLangFlag([]string{"--lang=fr"}); err == nil {
		t.Fatal("unknown language should fail")
	}
	reset()
}
