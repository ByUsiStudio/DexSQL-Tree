// Command dexgen writes the bundled demo DEX/APK used by the README and tests.
//
//	go run ./cmd/dexgen -out testdata
package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dexsql-tree/dexsql-tree/internal/dextest"
)

func main() {
	out := flag.String("out", "testdata", "output directory")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		panic(err)
	}
	dexBytes := dextest.HelloDex()
	dexPath := filepath.Join(*out, "demo.dex")
	if err := os.WriteFile(dexPath, dexBytes, 0o644); err != nil {
		panic(err)
	}
	apkPath := filepath.Join(*out, "demo.apk")
	f, err := os.Create(apkPath)
	if err != nil {
		panic(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("classes.dex")
	if err != nil {
		panic(err)
	}
	if _, err := w.Write(dexBytes); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %s (%d bytes) and %s\n", dexPath, len(dexBytes), apkPath)
}
