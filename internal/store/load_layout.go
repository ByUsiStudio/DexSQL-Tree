package store

import (
	"github.com/dexsql-tree/dexsql-tree/internal/dex"
)

func (l *loader) layout() error {
	for _, f := range l.s.Container.Files {
		for i, m := range f.MapItems {
			if err := l.ins("map_items", int64(f.FileID), int64(i), int64(m.TypeCode),
				int64(m.Size), int64(m.Offset)); err != nil {
				return err
			}
		}
		if err := l.typeLists(f); err != nil {
			return err
		}
		if err := l.idItems(f); err != nil {
			return err
		}
		for _, mh := range f.MethodHandles {
			if err := l.ins("method_handles", int64(f.FileID), int64(mh.Index),
				int64(mh.Type), int64(mh.MemberIdx)); err != nil {
				return err
			}
		}
		for _, cs := range f.CallSites {
			var mh, name, proto any
			if cs.HasHandle {
				mh = int64(cs.MethodHandleIdx)
			}
			if cs.HasName {
				name = int64(cs.NameIdx)
			}
			if cs.HasProto {
				proto = int64(cs.ProtoIdx)
			}
			if err := l.ins("call_sites", int64(f.FileID), int64(cs.Index), int64(cs.Off),
				int64(cs.ValueCount), mh, name, proto); err != nil {
				return err
			}
			for i, v := range cs.Args {
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
				if err := l.ins("call_site_args", int64(f.FileID), int64(cs.Index), int64(cs.Off),
					int64(i), v.Kind, vidx, vi64, vf64); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (l *loader) typeLists(f *dex.File) error {
	seen := map[uint32]bool{}
	add := func(off uint32, entries []uint16) error {
		if off == 0 || seen[off] {
			return nil
		}
		seen[off] = true
		end := off + 4 + uint32(len(entries))*2
		if err := l.ins("type_lists", int64(f.FileID), int64(off), int64(end), int64(len(entries))); err != nil {
			return err
		}
		for i, t := range entries {
			if err := l.ins("type_list_entries", int64(f.FileID), int64(off), int64(i), int64(t)); err != nil {
				return err
			}
		}
		return nil
	}
	for _, p := range f.Protos {
		if err := add(p.ParametersOff, p.ParamTypeIdx); err != nil {
			return err
		}
	}
	for i := range f.Classes {
		if err := add(f.Classes[i].InterfacesOff, f.Classes[i].Interfaces); err != nil {
			return err
		}
	}
	return nil
}

func (l *loader) idItems(f *dex.File) error {
	h := f.Header
	add := func(typeCode uint32, idx, itemOff, itemEnd uint32, dataOff any) error {
		return l.ins("id_items", int64(f.FileID), int64(typeCode), int64(idx), int64(itemOff), int64(itemEnd), dataOff)
	}
	for i := uint32(0); i < h.StringCount; i++ {
		off := h.StringIDsOff + i*4
		var data any
		if int(i) < len(f.StringOffsets) {
			data = int64(f.StringOffsets[i])
		}
		if err := add(0x0001, i, off, off+4, data); err != nil {
			return err
		}
	}
	for i := uint32(0); i < h.TypeCount; i++ {
		off := h.TypeIDsOff + i*4
		if err := add(0x0002, i, off, off+4, nil); err != nil {
			return err
		}
	}
	for i := uint32(0); i < h.ProtoCount; i++ {
		off := h.ProtoIDsOff + i*12
		var data any
		if int(i) < len(f.Protos) && f.Protos[i].ParametersOff != 0 {
			data = int64(f.Protos[i].ParametersOff)
		}
		if err := add(0x0003, i, off, off+12, data); err != nil {
			return err
		}
	}
	for i := uint32(0); i < h.FieldCount; i++ {
		off := h.FieldIDsOff + i*8
		if err := add(0x0004, i, off, off+8, nil); err != nil {
			return err
		}
	}
	for i := uint32(0); i < h.MethodCount; i++ {
		off := h.MethodIDsOff + i*8
		if err := add(0x0005, i, off, off+8, nil); err != nil {
			return err
		}
	}
	for i := uint32(0); i < h.ClassCount; i++ {
		off := h.ClassDefsOff + i*32
		var data any
		if int(i) < len(f.Classes) && f.Classes[i].ClassDataOff != 0 {
			data = int64(f.Classes[i].ClassDataOff)
		}
		if err := add(0x0006, i, off, off+32, data); err != nil {
			return err
		}
	}
	return nil
}
