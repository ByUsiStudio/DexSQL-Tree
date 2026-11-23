package dex

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/byusi-studio/dexsql-tree/internal/dextest"
)

// A DEX 041 container holds several logical images after one header. Splitting
// must keep the images separate and number them classes.dex#1, #2 so file_id
// stays stable for callers.
func TestDex041SplitsLogicalImages(t *testing.T) {
	one := dextest.HelloDex()
	two := dextest.HelloDex()
	buf := append(append([]byte{}, one...), two...)
	copy(buf[4:8], []byte("041\x00"))
	binary.LittleEndian.PutUint32(buf[0x2c:], 2) // container_size
	binary.LittleEndian.PutUint32(buf[0x30:], 0) // header_offset

	c, err := ParseContainer(buf, "multi.dex")
	if err != nil {
		t.Fatalf("ParseContainer: %v", err)
	}
	if len(c.Files) != 2 {
		t.Fatalf("want 2 logical images, got %d", len(c.Files))
	}
	for i, f := range c.Files {
		if f.FileID != uint32(i) {
			t.Errorf("file %d has id %d", i, f.FileID)
		}
		if f.Version == "041" && i > 0 {
			t.Errorf("second image should be the original 035 header, got %s", f.Version)
		}
	}
	if c.Files[1].EntryName != "multi.dex#2" {
		t.Errorf("want entry name multi.dex#2, got %q", c.Files[1].EntryName)
	}
}

// A plain image must not be split, even though its version may look similar.
func TestDex035StaysSingle(t *testing.T) {
	c, err := ParseContainer(dextest.HelloDex(), "one.dex")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Files) != 1 {
		t.Fatalf("want 1 image, got %d", len(c.Files))
	}
}

func TestHiddenApiFlagsDecode(t *testing.T) {
	f := &File{
		Classes: []ClassDef{{
			Descriptor: "Lcom/example/Hidden;",
			ClassData: &ClassData{
				StaticFields:   []EncodedField{{FieldIdx: 11, Kind: "static"}},
				InstanceFields: []EncodedField{{FieldIdx: 12, Kind: "instance"}},
				DirectMethods:  []EncodedMethod{{MethodIdx: 21, Kind: "direct"}},
				VirtualMethods: []EncodedMethod{{MethodIdx: 22, Kind: "virtual"}},
			},
		}},
		MapItems: []MapItem{{TypeCode: TypeHiddenApiClassData, Offset: 64}},
	}
	data := make([]byte, 256)
	binary.LittleEndian.PutUint32(data[64:], 12) // section size (header included)
	// One flag word per member: static field sdk, instance field unrestricted,
	// direct method blocked, virtual method unsupported, then the 0 terminator.
	data[68] = hiddenSDK
	data[69] = 0
	data[70] = hiddenBlocked
	data[71] = hiddenUnsupported
	data[72] = 0

	f.parseHiddenApi(data)
	if len(f.HiddenApiFlags) != 3 {
		t.Fatalf("want 3 restricted members, got %d", len(f.HiddenApiFlags))
	}
	want := []struct {
		kind        string
		idx         uint32
		restriction string
	}{
		{"field", 11, "sdk"},
		{"method", 21, "blocked"},
		{"method", 22, "unsupported"},
	}
	for i, w := range want {
		got := f.HiddenApiFlags[i]
		if got.Kind != w.kind || got.MemberIdx != w.idx || got.Restriction != w.restriction {
			t.Errorf("flag %d = %s/%d/%s, want %s/%d/%s", i,
				got.Kind, got.MemberIdx, got.Restriction, w.kind, w.idx, w.restriction)
		}
		if got.ClassDesc != "Lcom/example/Hidden;" {
			t.Errorf("flag %d lost the class descriptor: %q", i, got.ClassDesc)
		}
	}
}

// A truncated table must produce a warning instead of panicking or inventing
// rows far beyond the class list.
func TestHiddenApiTruncatedIsSafe(t *testing.T) {
	f := &File{
		Classes: []ClassDef{{
			Descriptor: "LA;",
			ClassData: &ClassData{
				DirectMethods: []EncodedMethod{{MethodIdx: 1}, {MethodIdx: 2}},
			},
		}},
		MapItems: []MapItem{{TypeCode: TypeHiddenApiClassData, Offset: 8}},
	}
	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[8:], 5) // claims 5 bytes: room for one value
	data[12] = hiddenBlocked
	f.parseHiddenApi(data)
	if len(f.HiddenApiFlags) != 1 {
		t.Errorf("want 1 decoded flag, got %d", len(f.HiddenApiFlags))
	}
	if len(f.Warnings) == 0 {
		t.Error("want a warning about the truncated table")
	}
}

func TestRestrictionLabel(t *testing.T) {
	cases := map[uint32]string{
		0:                            "",
		hiddenSDK:                    "sdk",
		hiddenUnsupported:            "unsupported",
		hiddenBlocked:                "blocked",
		hiddenCorePlatform:           "core-platform",
		hiddenTest:                   "test",
		hiddenMaxTargetQ | hiddenSDK: "unsupported",
	}
	for flags, want := range cases {
		if got := restrictionLabel(flags); got != want {
			t.Errorf("flags 0x%x: got %q, want %q", flags, got, want)
		}
	}
}

// Multi-dex archives are decoded concurrently; the resulting order and file ids
// must be identical to the serial path regardless of entry order in the zip.
func TestMultiDexIsOrderedAndStable(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"classes3.dex", "classes.dex", "classes2.dex"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(dextest.HelloDex()); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	serial, err := ParseContainer(buf.Bytes(), "app.apk", Options{Jobs: 1})
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	parallel, err := ParseContainer(buf.Bytes(), "app.apk", Options{Jobs: 3})
	if err != nil {
		t.Fatalf("parallel: %v", err)
	}
	want := []string{"classes.dex", "classes2.dex", "classes3.dex"}
	if len(serial.Files) != len(want) {
		t.Fatalf("want %d dex files, got %d", len(want), len(serial.Files))
	}
	for i, name := range want {
		if serial.Files[i].EntryName != name {
			t.Errorf("serial order %d = %q, want %q", i, serial.Files[i].EntryName, name)
		}
		if parallel.Files[i].EntryName != name {
			t.Errorf("parallel order %d = %q, want %q", i, parallel.Files[i].EntryName, name)
		}
		if parallel.Files[i].FileID != uint32(i) {
			t.Errorf("parallel file %d has id %d", i, parallel.Files[i].FileID)
		}
		if len(serial.Files[i].Methods) != len(parallel.Files[i].Methods) {
			t.Errorf("file %d parsed differently: %d vs %d methods", i,
				len(serial.Files[i].Methods), len(parallel.Files[i].Methods))
		}
	}
}
