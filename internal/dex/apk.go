package dex

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Container is one input file (APK, JAR, ZIP or bare DEX) after loading.
type Container struct {
	Path      string
	Files     []*File
	Warnings  []string
	DexBytes  int64
	EntryName []string
}

// Load reads a DEX image or every *.dex entry of an APK/ZIP/JAR. Entries are
// ordered classes.dex, classes2.dex, ... so file_id assignment is stable.
func Load(path string, opts ...Options) (*Container, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseContainer(data, path, opts...)
}

// ParseContainer decodes an in-memory DEX or ZIP image.
func ParseContainer(data []byte, path string, opts ...Options) (*Container, error) {
	c := &Container{Path: path}
	if bytes.HasPrefix(data, []byte("dex\n")) {
		f, err := Parse(data, path, filepath.Base(path), 0, opts...)
		if err != nil {
			return nil, err
		}
		c.Files = []*File{f}
		c.DexBytes = int64(len(data))
		c.EntryName = []string{filepath.Base(path)}
		return c, nil
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%s: not a dex or zip container: %w", path, err)
	}
	var entries []*zip.File
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		if strings.HasSuffix(name, ".dex") && !strings.HasSuffix(name, "/") {
			entries = append(entries, f)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return dexOrder(entries[i].Name) < dexOrder(entries[j].Name)
	})
	for _, e := range entries {
		rc, err := e.Open()
		if err != nil {
			c.Warnings = append(c.Warnings, fmt.Sprintf("%s: %v", e.Name, err))
			continue
		}
		buf, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			c.Warnings = append(c.Warnings, fmt.Sprintf("%s: %v", e.Name, err))
			continue
		}
		f, err := Parse(buf, path, e.Name, uint32(len(c.Files)), opts...)
		if err != nil {
			c.Warnings = append(c.Warnings, fmt.Sprintf("%s: %v", e.Name, err))
			continue
		}
		c.Files = append(c.Files, f)
		c.DexBytes += int64(len(buf))
		c.EntryName = append(c.EntryName, e.Name)
	}
	if len(c.Files) == 0 {
		return nil, fmt.Errorf("%s: no readable dex entry", path)
	}
	return c, nil
}

// dexOrder sorts classes.dex first, then classesN.dex by N, then other names.
func dexOrder(name string) string {
	base := strings.ToLower(filepath.Base(name))
	if base == "classes.dex" {
		return "0000000"
	}
	if strings.HasPrefix(base, "classes") && strings.HasSuffix(base, ".dex") {
		n := strings.TrimSuffix(strings.TrimPrefix(base, "classes"), ".dex")
		return fmt.Sprintf("0%07s", n)
	}
	return "1" + base
}

// FindMethod resolves a method descriptor. It returns the first match across
// all loaded dex files.
func (c *Container) FindMethod(desc string) (*File, uint32, bool) {
	for _, f := range c.Files {
		for i := range f.Methods {
			if f.Methods[i].Descriptor == desc {
				return f, uint32(i), true
			}
		}
	}
	return nil, 0, false
}

// Stats is a small summary used by the CLI and API.
type Stats struct {
	Files     int      `json:"files"`
	DexBytes  int64    `json:"dexBytes"`
	Strings   int      `json:"strings"`
	Types     int      `json:"types"`
	Methods   int      `json:"methods"`
	Classes   int      `json:"classes"`
	Fields    int      `json:"fields"`
	CodeItems int      `json:"codeItems"`
	Warnings  []string `json:"warnings,omitempty"`
}

// Stats aggregates counters across all loaded dex files.
func (c *Container) Stats() Stats {
	s := Stats{Files: len(c.Files), DexBytes: c.DexBytes, Warnings: c.Warnings}
	for _, f := range c.Files {
		s.Strings += len(f.Strings)
		s.Types += len(f.TypeDescs)
		s.Methods += len(f.Methods)
		s.Classes += len(f.Classes)
		s.Fields += len(f.Fields)
		s.CodeItems += len(f.CodeItems)
	}
	return s
}

// Merge combines several containers into one, renumbering file ids so they are
// unique across inputs. The underlying dex.File pointers are shared.
func Merge(cs []*Container) *Container {
	out := &Container{}
	for _, c := range cs {
		if c == nil {
			continue
		}
		if out.Path == "" {
			out.Path = c.Path
		} else {
			out.Path += "," + c.Path
		}
		out.Warnings = append(out.Warnings, c.Warnings...)
		out.DexBytes += c.DexBytes
		for _, f := range c.Files {
			f.FileID = uint32(len(out.Files))
			out.Files = append(out.Files, f)
			out.EntryName = append(out.EntryName, f.EntryName)
		}
	}
	return out
}
