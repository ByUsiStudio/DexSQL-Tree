// Package i18n selects English or Chinese text for the CLI, the HTTP API and
// the MCP server. Selection is explicit: an environment variable or the
// --lang flag decides, so output stays deterministic in scripts and tests.
package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Environment variables understood by the CLI. EnvZH is the documented name;
// EnvZHAlias exists because some shells and CI systems keep the hyphen when a
// variable is exported from a .env file.
const (
	EnvZH      = "DEXSQL_TREE_ZH"
	EnvZHAlias = "DEXSQL-TREE_ZH"
	EnvLang    = "DEXSQL_TREE_LANG"
)

var (
	mu       sync.Mutex
	resolved bool
	zh       bool
	source   = "default"
)

// On reports whether Chinese output is selected.
func On() bool {
	mu.Lock()
	defer mu.Unlock()
	resolve()
	return zh
}

// Lang returns "zh" or "en".
func Lang() string {
	if On() {
		return "zh"
	}
	return "en"
}

// Source names what decided the language: an environment variable name,
// "flag" for --lang, or "default".
func Source() string {
	mu.Lock()
	defer mu.Unlock()
	resolve()
	return source
}

// Set forces a language, taking precedence over the environment.
func Set(on bool) {
	mu.Lock()
	defer mu.Unlock()
	resolved = true
	zh = on
	source = "flag"
}

func resolve() {
	if resolved {
		return
	}
	resolved = true
	on, src := Detect()
	zh, source = on, src
}

// Detect reads the environment without mutating state.
func Detect() (bool, string) {
	if v, ok := os.LookupEnv(EnvLang); ok {
		if on, known := parseTruthy(v); known {
			return on, EnvLang
		}
	}
	for _, name := range []string{EnvZH, EnvZHAlias} {
		if v, ok := os.LookupEnv(name); ok && strings.TrimSpace(v) != "" {
			if on, known := parseTruthy(v); known {
				return on, name
			}
		}
	}
	return false, "default"
}

// parseTruthy accepts the spellings people actually use in shell profiles.
func parseTruthy(v string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "y", "on", "zh", "zh-cn", "zh_cn", "zh-hans", "cn", "中文":
		return true, true
	case "false", "0", "no", "n", "off", "en", "en-us", "en_us", "english":
		return false, true
	}
	return false, false
}

// T returns the Chinese string when Chinese is selected, otherwise English.
func T(en, zhText string) string {
	if On() {
		return zhText
	}
	return en
}

// ParseFlagValue validates a --lang value.
func ParseFlagValue(v string) (bool, error) {
	if on, known := parseTruthy(v); known {
		return on, nil
	}
	return false, fmt.Errorf("unknown language %q (use zh or en)", v)
}

// ConsumeLangFlag removes --lang/--zh/--en from args so subcommands can parse
// the rest with the standard flag package.
func ConsumeLangFlag(args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--zh":
			Set(true)
		case a == "--en":
			Set(false)
		case a == "--lang" || a == "-lang":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--lang needs a value (zh or en)")
			}
			i++
			on, err := ParseFlagValue(args[i])
			if err != nil {
				return nil, err
			}
			Set(on)
		case strings.HasPrefix(a, "--lang="):
			on, err := ParseFlagValue(strings.TrimPrefix(a, "--lang="))
			if err != nil {
				return nil, err
			}
			Set(on)
		case strings.HasPrefix(a, "-lang="):
			on, err := ParseFlagValue(strings.TrimPrefix(a, "-lang="))
			if err != nil {
				return nil, err
			}
			Set(on)
		default:
			out = append(out, a)
		}
	}
	return out, nil
}
