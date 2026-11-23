package store

import (
	"sort"

	"github.com/byusi-studio/dexsql-tree/internal/dex"
)

type annOwner struct {
	kind    string
	idx     uint32
	classIx uint32
	desc    string
	param   int64
}

func retention(v uint32) string {
	switch v {
	case 1:
		return "runtime"
	case 2:
		return "system"
	default:
		return "class"
	}
}

func (l *loader) annotations() error {
	// The annotation tables are the largest part of the catalog by row count and
	// the least queried, so the fast profile skips their expansion while still
	// writing static field initial values (which are cheap and useful).
	full := l.profile != ProfileFast
	for _, f := range l.s.Container.Files {
		if !full {
			if err := l.staticValues(f); err != nil {
				return err
			}
			continue
		}
		owners := map[uint32][]annOwner{}
		addSet := func(off uint32, o annOwner) {
			if off == 0 {
				return
			}
			for _, s := range f.AnnotationSets {
				if s.Off != off {
					continue
				}
				for _, aoff := range s.Elements {
					owners[aoff] = append(owners[aoff], o)
				}
			}
		}
		for _, d := range f.AnnotationDirs {
			if int(d.ClassIdx) >= len(f.TypeDescs) {
				continue
			}
			classDesc := f.TypeDescs[d.ClassIdx]
			if d.HasClassSet {
				addSet(d.ClassSetOff, annOwner{kind: "class", idx: d.ClassIdx, classIx: d.ClassIdx, desc: classDesc, param: -1})
			}
			for _, e := range d.Fields {
				desc := ""
				if int(e.EntryIdx) < len(f.Fields) {
					desc = f.Fields[e.EntryIdx].Descriptor()
				}
				addSet(e.AnnotationSetOff, annOwner{kind: "field", idx: e.EntryIdx, classIx: d.ClassIdx, desc: desc, param: -1})
			}
			for _, e := range d.Methods {
				desc := ""
				if int(e.EntryIdx) < len(f.Methods) {
					desc = f.Methods[e.EntryIdx].Descriptor
				}
				addSet(e.AnnotationSetOff, annOwner{kind: "method", idx: e.EntryIdx, classIx: d.ClassIdx, desc: desc, param: -1})
			}
			for _, e := range d.Parameters {
				desc := ""
				if int(e.EntryIdx) < len(f.Methods) {
					desc = f.Methods[e.EntryIdx].Descriptor
				}
				for _, rl := range f.AnnotationRefLists {
					if rl.Off != e.RefListOff {
						continue
					}
					for i, setOff := range rl.Sets {
						if setOff == 0 {
							continue
						}
						addSet(setOff, annOwner{kind: "parameter", idx: e.EntryIdx, classIx: d.ClassIdx, desc: desc, param: int64(i)})
					}
				}
			}
		}

		anns := append([]*dex.Annotation{}, f.Annotations...)
		sort.Slice(anns, func(i, j int) bool { return anns[i].Off < anns[j].Off })
		for _, a := range anns {
			if err := l.ins("annotations", int64(f.FileID), int64(a.Off), int64(a.Visibility),
				int64(a.TypeIdx), int64(a.ElementCount)); err != nil {
				return err
			}
			for _, el := range a.Elements {
				var nameIdx any
				if el.HasName {
					nameIdx = int64(el.NameIdx)
				}
				var vidx any
				if el.Value.HasIdx {
					vidx = int64(el.Value.ValueIdx)
				}
				var vi64, vf64 any
				if isNumericKind(el.Value.Kind) {
					vi64 = el.Value.I64
				}
				if el.Value.HasFloat {
					vf64 = el.Value.F64
				}
				if err := l.ins("annotation_elements", int64(f.FileID), int64(a.Off), int64(el.Depth),
					int64(el.Ordinal), nameIdx, int64(el.ValueOff), int64(el.ValueEndOff), el.Value.Kind,
					vidx, vi64, vf64); err != nil {
					return err
				}
			}
			os := owners[a.Off]
			if len(os) == 0 {
				continue
			}
			for _, o := range os {
				top := 0
				for _, el := range a.Elements {
					if el.Depth != 0 {
						continue
					}
					top++
					var paramIdx any
					if o.param >= 0 {
						paramIdx = o.param
					}
					var vidx, vi64, vf64 any
					if el.Value.HasIdx {
						vidx = int64(el.Value.ValueIdx)
					}
					if isNumericKind(el.Value.Kind) {
						vi64 = el.Value.I64
					}
					if el.Value.HasFloat {
						vf64 = el.Value.F64
					}
					if err := l.ins("annotation_elements_resolved", int64(f.FileID), int64(o.classIx),
						o.kind, int64(o.idx), o.desc, paramIdx, a.TypeDesc, int64(a.Visibility),
						int64(a.ElementCount), int64(a.Off), int64(el.Ordinal), el.Name, el.Value.Kind,
						vidx, vi64, vf64, retention(a.Visibility)); err != nil {
						return err
					}
				}
				if top == 0 {
					var paramIdx any
					if o.param >= 0 {
						paramIdx = o.param
					}
					if err := l.ins("annotation_elements_resolved", int64(f.FileID), int64(o.classIx),
						o.kind, int64(o.idx), o.desc, paramIdx, a.TypeDesc, int64(a.Visibility),
						int64(a.ElementCount), int64(a.Off), nil, nil, nil, nil, nil, nil,
						retention(a.Visibility)); err != nil {
						return err
					}
				}
				for _, el := range a.Elements {
					var nameIdx any
					if el.HasName {
						nameIdx = int64(el.NameIdx)
					}
					var paramIdx any
					if o.param >= 0 {
						paramIdx = o.param
					}
					var vi64, vf64 any
					if isNumericKind(el.Value.Kind) {
						vi64 = el.Value.I64
					}
					if el.Value.HasFloat {
						vf64 = el.Value.F64
					}
					if err := l.ins("annotation_values", int64(f.FileID), int64(o.classIx), o.kind,
						int64(o.idx), o.desc, paramIdx, int64(a.Off), int64(a.TypeIdx), a.TypeDesc,
						int64(el.Depth), int64(el.Ordinal), nameIdx, el.Name, el.Value.Kind, vi64, vf64); err != nil {
						return err
					}
				}
			}
		}

		for _, d := range f.AnnotationDirs {
			var classSet any
			if d.HasClassSet {
				classSet = int64(d.ClassSetOff)
			}
			if err := l.ins("annotation_directories", int64(f.FileID), int64(d.ClassIdx),
				int64(d.AnnotationsOff), classSet, int64(len(d.Fields)), int64(len(d.Methods)),
				int64(len(d.Parameters))); err != nil {
				return err
			}
			for _, e := range d.Fields {
				if err := l.ins("annotation_directory_entries", int64(f.FileID), int64(d.ClassIdx),
					int64(d.AnnotationsOff), "field", int64(e.EntryIdx), int64(e.AnnotationSetOff), nil); err != nil {
					return err
				}
			}
			for _, e := range d.Methods {
				if err := l.ins("annotation_directory_entries", int64(f.FileID), int64(d.ClassIdx),
					int64(d.AnnotationsOff), "method", int64(e.EntryIdx), int64(e.AnnotationSetOff), nil); err != nil {
					return err
				}
			}
			for _, e := range d.Parameters {
				if err := l.ins("annotation_directory_entries", int64(f.FileID), int64(d.ClassIdx),
					int64(d.AnnotationsOff), "parameter", int64(e.EntryIdx), nil, int64(e.RefListOff)); err != nil {
					return err
				}
			}
		}
		for _, s := range f.AnnotationSets {
			if err := l.ins("annotation_sets", int64(f.FileID), int64(s.Off), int64(s.Size)); err != nil {
				return err
			}
			for i, aoff := range s.Elements {
				if err := l.ins("annotation_set_entries", int64(f.FileID), int64(s.Off), int64(i), int64(aoff)); err != nil {
					return err
				}
			}
		}
		for _, rl := range f.AnnotationRefLists {
			if err := l.ins("annotation_set_ref_lists", int64(f.FileID), int64(rl.Off), int64(len(rl.Sets))); err != nil {
				return err
			}
			for i, setOff := range rl.Sets {
				var v any
				if rl.HasSet[i] {
					v = int64(setOff)
				}
				if err := l.ins("annotation_set_refs", int64(f.FileID), int64(rl.Off), int64(i), v); err != nil {
					return err
				}
			}
		}

		for _, ea := range f.EncodedArrays {
			if err := l.ins("encoded_arrays", int64(f.FileID), int64(ea.Index), int64(ea.Off),
				int64(ea.EndOff), int64(len(ea.Values))); err != nil {
				return err
			}
			if err := l.ins("encoded_array_owners", int64(f.FileID), int64(ea.Off), ea.OwnerKind, int64(ea.OwnerIdx)); err != nil {
				return err
			}
			for _, el := range ea.All {
				var nameIdx any
				if el.HasName {
					nameIdx = int64(el.NameIdx)
				}
				var vidx, vi64, vf64 any
				if el.Value.HasIdx {
					vidx = int64(el.Value.ValueIdx)
				}
				if isNumericKind(el.Value.Kind) {
					vi64 = el.Value.I64
				}
				if el.Value.HasFloat {
					vf64 = el.Value.F64
				}
				if err := l.ins("encoded_array_values", int64(f.FileID), int64(ea.Off), int64(el.Depth),
					int64(el.Ordinal), nameIdx, int64(el.ValueOff), int64(el.ValueEndOff), el.Value.Kind,
					vidx, vi64, vf64); err != nil {
					return err
				}
			}
		}

	}
	return nil
}

// staticValues writes class_static_values without touching annotations.
func (l *loader) staticValues(f *dex.File) error {
	for i := range f.Classes {
		c := &f.Classes[i]
		if c.ClassData == nil || len(c.StaticValues) == 0 {
			continue
		}
		for j, v := range c.StaticValues {
			var fieldIdx any
			if j < len(c.ClassData.StaticFields) {
				fieldIdx = int64(c.ClassData.StaticFields[j].FieldIdx)
			}
			var vidx, vi64, vf64 any
			if v.HasIdx {
				vidx = int64(v.ValueIdx)
			}
			if isNumericKind(v.Kind) {
				vi64 = v.I64
			}
			if v.HasFloat {
				vf64 = v.F64
			}
			if err := l.ins("class_static_values", int64(f.FileID), int64(c.ClassIdx),
				int64(c.StaticValuesOff), int64(j), fieldIdx, v.Kind, vidx, vi64, vf64); err != nil {
				return err
			}
		}
	}
	return nil
}

func isNumericKind(k string) bool {
	switch k {
	case "byte", "short", "char", "int", "long", "float", "double":
		return true
	}
	return false
}
