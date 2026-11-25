package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoopbackAddr(t *testing.T) {
	loopback := []string{"127.0.0.1:8765", "127.1.2.3:1", "localhost:8765", "[::1]:8765", ":8765"}
	for _, addr := range loopback {
		if !loopbackAddr(addr) {
			t.Errorf("%s should count as loopback", addr)
		}
	}
	for _, addr := range []string{"0.0.0.0:8765", "192.168.1.10:8765", "example.com:443"} {
		if loopbackAddr(addr) {
			t.Errorf("%s should not count as loopback", addr)
		}
	}
}

// The language block must be replaceable and removable without touching the
// rest of a shell profile.
func TestWriteBlockRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	original := "export PATH=/usr/bin\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := writeBlock(path, "export DEXSQL_TREE_ZH=true", false); err != nil || !changed {
		t.Fatalf("write: changed=%v err=%v", changed, err)
	}
	if changed, err := writeBlock(path, "export DEXSQL_TREE_ZH=true", false); err != nil || changed {
		t.Fatalf("rewrite should be a no-op: changed=%v err=%v", changed, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), blockStart) != 1 {
		t.Fatalf("block written %d times:\n%s", strings.Count(string(b), blockStart), b)
	}
	if !strings.Contains(string(b), "export PATH=/usr/bin") {
		t.Fatalf("existing content lost:\n%s", b)
	}
	if changed, err := writeBlock(path, "", true); err != nil || !changed {
		t.Fatalf("remove: changed=%v err=%v", changed, err)
	}
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), blockStart) {
		t.Fatalf("block left behind:\n%s", b)
	}
	if strings.TrimSpace(string(b)) != strings.TrimSpace(original) {
		t.Fatalf("profile should be restored, got %q", b)
	}
}

// The size line is the first thing a newcomer reads; raw counts are hard to
// size at a glance.
func TestThousands(t *testing.T) {
	for in, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 64586: "64,586", 1234567: "1,234,567"} {
		if got := thousands(in); got != want {
			t.Errorf("thousands(%d) = %q, want %q", in, got, want)
		}
	}
}

// Without an input the first-run command must say so instead of opening an
// empty catalog.
func TestStartNeedsInput(t *testing.T) {
	if err := cmdStart(nil); err == nil {
		t.Fatal("start without --input should fail")
	}
}
