package store

import (
	"encoding/hex"
	"fmt"

	"github.com/dexsql-tree/dexsql-tree/internal/dex"
)

type methodDef struct {
	flags uint32
	kind  string
}

func methodDefs(f *dex.File) map[uint32]methodDef {
	m := map[uint32]methodDef{}
	for i := range f.Classes {
		cd := f.Classes[i].ClassData
		if cd == nil {
			continue
		}
		for _, em := range cd.DirectMethods {
			m[em.MethodIdx] = methodDef{em.AccessFlags, em.Kind}
		}
		for _, em := range cd.VirtualMethods {
			m[em.MethodIdx] = methodDef{em.AccessFlags, em.Kind}
		}
	}
	return m
}

func (l *loader) dexFiles() error {
	for _, f := range l.s.Container.Files {
		h := f.Header
		if err := l.ins("dex_files",
			int64(f.FileID), int64(f.FileID), f.SourcePath, f.EntryName, f.ByteLen,
			f.Version, int64(h.Checksum), hex.EncodeToString(h.Signature[:]), int64(h.FileSize), int64(h.HeaderSize),
			int64(h.EndianTag), int64(h.LinkSize), int64(h.LinkOff), int64(h.MapOff), int64(h.StringCount),
			int64(h.StringIDsOff), int64(h.TypeCount), int64(h.TypeIDsOff), int64(h.ProtoCount),
			int64(h.ProtoIDsOff), int64(h.FieldCount), int64(h.FieldIDsOff), int64(h.MethodCount),
			int64(h.MethodIDsOff), int64(h.ClassCount), int64(h.ClassDefsOff), int64(h.DataSize), int64(h.DataOff),
		); err != nil {
			return err
		}
	}
	return nil
}

func (l *loader) strings() error {
	for _, f := range l.s.Container.Files {
		for i, s := range f.Strings {
			if err := l.ins("strings", int64(f.FileID), int64(i), s); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *loader) types() error {
	for _, f := range l.s.Container.Files {
		for i, d := range f.TypeDescs {
			if err := l.ins("type_ids", int64(f.FileID), int64(i), int64(f.TypeStringIdx[i])); err != nil {
				return err
			}
			if err := l.ins("types", int64(f.FileID), int64(i), d, dex.JavaName(d)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *loader) protos() error {
	for _, f := range l.s.Container.Files {
		for _, p := range f.Protos {
			if err := l.ins("proto_ids", int64(f.FileID), int64(p.Index), int64(p.ShortyIdx),
				int64(p.ReturnTypeIdx), int64(p.ParametersOff)); err != nil {
				return err
			}
			joined := ""
			for i, d := range p.ParamDescs {
				if i > 0 {
					joined += ","
				}
				joined += d
			}
			if err := l.ins("protos", int64(f.FileID), int64(p.Index), int64(p.ShortyIdx), p.Shorty,
				int64(p.ReturnTypeIdx), p.ReturnDesc, int64(p.ParametersOff), int64(len(p.ParamTypeIdx)), joined); err != nil {
				return err
			}
			for i, t := range p.ParamTypeIdx {
				if err := l.ins("proto_params", int64(f.FileID), int64(p.Index), int64(i), int64(t),
					f.TypeDescs[t]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (l *loader) fields() error {
	for _, f := range l.s.Container.Files {
		for _, fd := range f.Fields {
			if err := l.ins("field_ids", int64(f.FileID), int64(fd.Index), int64(fd.ClassIdx),
				int64(fd.TypeIdx), int64(fd.NameIdx)); err != nil {
				return err
			}
			if err := l.ins("fields", int64(f.FileID), int64(fd.Index), int64(fd.ClassIdx), fd.ClassDesc,
				int64(fd.TypeIdx), fd.TypeDesc, fd.Name, fd.Descriptor()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *loader) methods() error {
	for _, f := range l.s.Container.Files {
		defs := methodDefs(f)
		for _, m := range f.Methods {
			if err := l.ins("method_ids", int64(f.FileID), int64(m.Index), int64(m.ClassIdx),
				int64(m.ProtoIdx), int64(m.NameIdx)); err != nil {
				return err
			}
			var access, opCount any
			var opHex any
			var kind any
			if d, ok := defs[m.Index]; ok {
				access = int64(d.flags)
				kind = d.kind
				if ci := f.CodeItems[m.Index]; ci != nil {
					opCount = int64(ci.OpcodeCount)
					opHex = ci.OpcodeHex
				} else {
					opCount = int64(0)
					opHex = ""
				}
			} else {
				access, opCount, opHex, kind = nil, nil, nil, nil
			}
			joined := joinComma(m.ParamDescs)
			if err := l.ins("methods", int64(f.FileID), int64(m.Index), int64(m.ClassIdx), m.ClassDesc,
				int64(m.ProtoIdx), m.Name, m.Descriptor, m.ReturnDesc, int64(len(m.ParamDescs)),
				joined, access, opCount, opHex, kind); err != nil {
				return err
			}
			if err := l.ins("method_opcodes", int64(f.FileID), int64(m.Index), opCount, opHex); err != nil {
				return err
			}
		}
	}
	return nil
}

func joinComma(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

func (l *loader) classes() error {
	for _, f := range l.s.Container.Files {
		for i := range f.Classes {
			c := &f.Classes[i]
			var superIdx any
			if c.HasSuperclass {
				superIdx = int64(c.SuperclassIdx)
			}
			var srcIdx any
			if c.HasSourceFile {
				srcIdx = int64(c.SourceFileIdx)
			}
			if err := l.ins("class_defs", int64(f.FileID), int64(i), int64(c.ClassIdx), int64(c.AccessFlags),
				superIdx, int64(c.InterfacesOff), srcIdx, int64(c.AnnotationsOff),
				int64(c.ClassDataOff), int64(c.StaticValuesOff)); err != nil {
				return err
			}
			var super any
			if c.HasSuperclass {
				super = c.SuperDescriptor
			}
			var src any
			if c.HasSourceFile {
				src = c.SourceFile
			}
			if err := l.ins("classes", int64(f.FileID), int64(i), int64(c.ClassIdx), c.Descriptor,
				int64(c.AccessFlags), super, int64(len(c.Interfaces)), src); err != nil {
				return err
			}
			for j, d := range c.InterfaceDescs {
				if err := l.ins("class_interfaces", int64(f.FileID), int64(c.ClassIdx), int64(j),
					int64(c.Interfaces[j]), d); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (l *loader) classMembers() error {
	for _, f := range l.s.Container.Files {
		for i := range f.Classes {
			c := &f.Classes[i]
			cd := c.ClassData
			if cd == nil {
				continue
			}
			if err := l.ins("class_data_items", int64(f.FileID), int64(c.ClassIdx), int64(cd.Off),
				int64(cd.EndOff), int64(len(cd.StaticFields)), int64(len(cd.InstanceFields)),
				int64(len(cd.DirectMethods)), int64(len(cd.VirtualMethods))); err != nil {
				return err
			}
			addField := func(ef dex.EncodedField) error {
				if int(ef.FieldIdx) >= len(f.Fields) {
					return nil
				}
				fd := f.Fields[ef.FieldIdx]
				if err := l.ins("class_data_fields", int64(f.FileID), int64(c.ClassIdx), int64(ef.FieldIdx),
					int64(ef.AccessFlags), ef.Kind); err != nil {
					return err
				}
				return l.ins("class_fields", int64(f.FileID), int64(c.ClassIdx), c.Descriptor,
					int64(ef.FieldIdx), fd.Name, fd.Descriptor(), ef.Kind, int64(ef.AccessFlags))
			}
			for _, ef := range cd.StaticFields {
				if err := addField(ef); err != nil {
					return err
				}
			}
			for _, ef := range cd.InstanceFields {
				if err := addField(ef); err != nil {
					return err
				}
			}
			addMethod := func(em dex.EncodedMethod) error {
				if int(em.MethodIdx) >= len(f.Methods) {
					return nil
				}
				m := f.Methods[em.MethodIdx]
				if err := l.ins("class_data_methods", int64(f.FileID), int64(c.ClassIdx), int64(em.MethodIdx),
					int64(em.AccessFlags), em.Kind); err != nil {
					return err
				}
				return l.ins("class_methods", int64(f.FileID), int64(c.ClassIdx), c.Descriptor,
					int64(em.MethodIdx), m.Name, m.Descriptor, em.Kind, int64(em.AccessFlags))
			}
			for _, em := range cd.DirectMethods {
				if err := addMethod(em); err != nil {
					return err
				}
			}
			for _, em := range cd.VirtualMethods {
				if err := addMethod(em); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// methodMeta returns (classIdx, descriptor, class descriptor) for a method idx.
func methodMeta(f *dex.File, idx uint32) (uint16, string, string, bool) {
	if int(idx) >= len(f.Methods) {
		return 0, "", "", false
	}
	m := f.Methods[idx]
	return m.ClassIdx, m.Descriptor, m.ClassDesc, true
}

func warnf(dst *[]string, format string, args ...any) {
	*dst = append(*dst, fmt.Sprintf(format, args...))
}
