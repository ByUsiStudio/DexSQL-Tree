package dex_test

import (
	"testing"

	"github.com/byusi-studio/dexsql-tree/internal/dex"
	"github.com/byusi-studio/dexsql-tree/internal/dextest"
)

// FuzzParseContainer feeds arbitrary bytes to the DEX/ZIP reader. The parser
// trusts nothing in the header, so every section offset is a chance to read
// out of bounds; a fuzz run is the cheapest way to keep that true.
//
//	go test ./internal/dex -run=^$ -fuzz=FuzzParseContainer -fuzztime=5m
func FuzzParseContainer(f *testing.F) {
	f.Add(dextest.HelloDex())
	f.Add([]byte("dex\n035\x00"))
	f.Add([]byte("PK\x03\x04"))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := dex.ParseContainer(data, "fuzz")
		if err != nil {
			return
		}
		if len(c.Files) == 0 {
			t.Fatalf("ParseContainer returned an empty container without an error")
		}
		for _, file := range c.Files {
			if len(file.Strings) != int(file.Header.StringCount) && file.Header.StringCount > 0 {
				// Decoding may stop early on a truncated pool, but it must not
				// invent more entries than the header claims.
				if len(file.Strings) > int(file.Header.StringCount) {
					t.Fatalf("decoded %d strings, header says %d", len(file.Strings), file.Header.StringCount)
				}
			}
			for _, m := range file.Methods {
				_ = m.Descriptor
			}
		}
		_ = c.Stats()
	})
}
