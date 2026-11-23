package dex

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
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
//
// Multi-dex archives are decoded concurrently: every DEX is an independent
// image, so the only shared state is the result slice, which is filled by
// index. Ordering is therefore identical to the serial version.
func ParseContainer(data []byte, path string, opts ...Options) (*Container, error) {
	var opt Options
	if len(opts) > 0 {
		opt = opts[0]
	}
	c := &Container{Path: path}
	if bytes.HasPrefix(data, []byte("dex\n")) {
		images := splitDexContainer(data)
		for i, img := range images {
			name := filepath.Base(path)
			if len(images) > 1 {
				name = fmt.Sprintf("%s#%d", name, i+1)
			}
			f, err := Parse(img, path, name, uint32(i), opt)
			if err != nil {
				if len(images) == 1 {
					return nil, err
				}
				c.Warnings = append(c.Warnings, fmt.Sprintf("%s: %v", name, err))
				continue
			}
			c.Files = append(c.Files, f)
			c.DexBytes += int64(len(img))
			c.EntryName = append(c.EntryName, name)
		}
		if len(c.Files) == 0 {
			return nil, fmt.Errorf("%s: no readable dex image", path)
		}
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

	type result struct {
		files  []*File
		bytes  int
		name   string
		failed error
	}
	results := make([]result, len(entries))
	jobs := opt.Jobs
	if jobs <= 0 {
		jobs = runtime.NumCPU()
	}
	if jobs > len(entries) {
		jobs = len(entries)
	}
	if jobs < 1 {
		jobs = 1
	}
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	for i, e := range entries {
		wg.Add(1)
		go func(i int, e *zip.File) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res := result{name: e.Name}
			rc, err := e.Open()
			if err != nil {
				res.failed = err
				results[i] = res
				return
			}
			buf, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				res.failed = err
				results[i] = res
				return
			}
			res.bytes = len(buf)
			images := splitDexContainer(buf)
			for j, img := range images {
				name := e.Name
				if len(images) > 1 {
					name = fmt.Sprintf("%s#%d", e.Name, j+1)
				}
				f, err := Parse(img, path, name, 0, opt)
				if err != nil {
					if len(images) == 1 {
						res.failed = err
					}
					break
				}
				res.files = append(res.files, f)
				res.name = name
			}
			results[i] = res
		}(i, e)
	}
	wg.Wait()

	for _, res := range results {
		if res.failed != nil {
			c.Warnings = append(c.Warnings, fmt.Sprintf("%s: %v", res.name, res.failed))
			continue
		}
		for _, f := range res.files {
			f.FileID = uint32(len(c.Files))
			c.Files = append(c.Files, f)
			c.DexBytes += int64(f.ByteLen)
			c.EntryName = append(c.EntryName, f.EntryName)
		}
	}
	if len(c.Files) == 0 {
		return nil, fmt.Errorf("%s: no readable dex entry", path)
	}
	return c, nil
}

// splitDexContainer expands a DEX 041 container into its logical images.
// Earlier versions return the single image unchanged. In a 041 container the
// link_size/link_off header fields are reused as container_size/header_offset,
// and the logical images follow each other, each starting with its own header.
func splitDexContainer(data []byte) [][]byte {
	single := [][]byte{data}
	if len(data) < 112 || string(data[4:7]) != "041" {
		return single
	}
	containerSize := leUint32(data[0x2c:]) // header field 11: link_size
	headerOff := leUint32(data[0x30:])     // header field 12: link_off
	// header_offset is relative to the container start, so the first header
	// legitimately sits at 0.
	if containerSize <= 1 || int(headerOff)+112 > len(data) {
		return single
	}
	var out [][]byte
	pos := int(headerOff)
	for i := uint32(0); i < containerSize && pos+112 <= len(data); i++ {
		size := int(leUint32(data[pos+0x20:])) // file_size
		if size < 112 || pos+size > len(data) {
			break
		}
		out = append(out, data[pos:pos+size])
		pos += size
	}
	if len(out) <= 1 {
		return single
	}
	return out
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
