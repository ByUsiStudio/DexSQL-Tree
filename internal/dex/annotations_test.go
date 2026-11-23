package dex

import (
	"encoding/binary"
	"testing"
)

// TestAnnotationDirectoryLayout guards the 16-byte header of
// annotations_directory_item: class_annotations_off, fields_size,
// annotated_methods_size, annotated_parameters_size. An earlier version also
// consumed a class_idx field, which shifted every entry and turned ordinary
// data into millions of bogus encoded values.
func TestAnnotationDirectoryLayout(t *testing.T) {
	raw := make([]byte, 256)
	le := binary.LittleEndian

	// Directory at 0x10: no class annotations, one annotated method.
	le.PutUint32(raw[0x10:], 0)    // class_annotations_off
	le.PutUint32(raw[0x14:], 0)    // fields_size
	le.PutUint32(raw[0x18:], 1)    // annotated_methods_size
	le.PutUint32(raw[0x1c:], 0)    // annotated_parameters_size
	le.PutUint32(raw[0x20:], 1)    // method_idx
	le.PutUint32(raw[0x24:], 0x40) // annotations_off -> annotation_set_item
	le.PutUint32(raw[0x40:], 1)    // set size
	le.PutUint32(raw[0x44:], 0x50) // annotation_off
	raw[0x50] = 0x01               // VISIBILITY_RUNTIME
	raw[0x51] = 0x00               // type_idx = 0
	raw[0x52] = 0x00               // zero elements

	f := &File{
		raw:       raw,
		TypeDescs: []string{"Ljava/lang/Deprecated;"},
		Classes:   []ClassDef{{Index: 0, AnnotationsOff: 0x10, Descriptor: "Ldemo/A;"}},
	}
	if err := f.parseAnnotations(raw); err != nil {
		t.Fatalf("parseAnnotations: %v", err)
	}
	if len(f.AnnotationDirs) != 1 {
		t.Fatalf("dirs = %d, want 1", len(f.AnnotationDirs))
	}
	if len(f.AnnotationSets) != 1 || len(f.AnnotationSets[0].Elements) != 1 {
		t.Fatalf("sets = %+v", f.AnnotationSets)
	}
	if got := f.AnnotationSets[0].Elements[0]; got != 0x50 {
		t.Fatalf("set element = %#x, want 0x50", got)
	}
	if len(f.Annotations) != 1 {
		t.Fatalf("annotations = %d, want 1", len(f.Annotations))
	}
	a := f.Annotations[0]
	if a.TypeDesc != "Ljava/lang/Deprecated;" || a.Visibility != 1 || a.ElementCount != 0 {
		t.Fatalf("annotation = %+v", a)
	}
	if f.AnnotationDirs[0].ClassIdx != 0 {
		t.Fatalf("class idx = %d, want 0", f.AnnotationDirs[0].ClassIdx)
	}
}
