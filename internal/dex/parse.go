package dex

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

// Parse decodes a single DEX image. sourcePath/entryName are carried into the
// output for provenance; fileID is assigned by the caller when several DEX
// files are loaded together.
// Options controls optional, potentially expensive parts of decoding.
type Options struct {
	// Debug materialises the line-number and local-variable tables. DEX debug
	// programs are often several times larger than the code itself, so this is
	// off by default.
	Debug bool
}

func Parse(data []byte, sourcePath, entryName string, fileID uint32, opts ...Options) (*File, error) {
	var opt Options
	if len(opts) > 0 {
		opt = opts[0]
	}
	if len(data) < 112 {
		return nil, fmt.Errorf("dex: file too small (%d bytes)", len(data))
	}
	if !bytes.HasPrefix(data, []byte("dex\n")) || data[7] != 0 {
		return nil, fmt.Errorf("dex: bad magic")
	}
	version := string(data[4:7])

	f := &File{
		raw:            data,
		debugPositions: opt.Debug,
		FileID:         fileID,
		SourcePath:     sourcePath,
		EntryName:      entryName,
		ByteLen:        int64(len(data)),
		Version:        version,
		CodeItems:      map[uint32]*CodeItem{},
		DebugInfos:     map[uint32]*DebugInfo{},
		DebugParams:    map[uint32][]uint32{},
		Positions:      map[uint32][]Position{},
		Locals:         map[uint32][]Local{},
	}

	r := &errReader{b: data}
	r.off = 8
	h := &f.Header
	h.Version = version
	h.Checksum = r.u4()
	copy(h.Signature[:], r.bytes(20))
	h.FileSize = r.u4()
	h.HeaderSize = r.u4()
	h.EndianTag = r.u4()
	h.LinkSize = r.u4()
	h.LinkOff = r.u4()
	h.MapOff = r.u4()
	h.StringCount = r.u4()
	h.StringIDsOff = r.u4()
	h.TypeCount = r.u4()
	h.TypeIDsOff = r.u4()
	h.ProtoCount = r.u4()
	h.ProtoIDsOff = r.u4()
	h.FieldCount = r.u4()
	h.FieldIDsOff = r.u4()
	h.MethodCount = r.u4()
	h.MethodIDsOff = r.u4()
	h.ClassCount = r.u4()
	h.ClassDefsOff = r.u4()
	h.DataSize = r.u4()
	h.DataOff = r.u4()
	if r.err != nil {
		return nil, r.err
	}
	if h.EndianTag != 0x12345678 {
		return nil, fmt.Errorf("dex: unsupported endian tag 0x%08x", h.EndianTag)
	}
	if int(h.FileSize) > len(data) {
		f.Warnings = append(f.Warnings, fmt.Sprintf("header file_size %d exceeds loaded bytes %d; using truncated image", h.FileSize, len(data)))
	}

	if err := f.parseStrings(data); err != nil {
		return nil, err
	}
	if err := f.parseTypes(data); err != nil {
		return nil, err
	}
	if err := f.parseProtos(data); err != nil {
		return nil, err
	}
	if err := f.parseFields(data); err != nil {
		return nil, err
	}
	if err := f.parseMethods(data); err != nil {
		return nil, err
	}
	if err := f.parseMap(data); err != nil {
		return nil, err
	}
	if err := f.parseClassDefs(data); err != nil {
		return nil, err
	}
	if err := f.parseMethodHandles(data); err != nil {
		return nil, err
	}
	if err := f.parseCallSites(data); err != nil {
		return nil, err
	}
	if err := f.parseAnnotations(data); err != nil {
		return nil, err
	}
	f.sortClassData()
	return f, nil
}

func (f *File) parseStrings(data []byte) error {
	h := f.Header
	f.Strings = make([]string, h.StringCount)
	f.StringOffsets = make([]uint32, h.StringCount)
	for i := uint32(0); i < h.StringCount; i++ {
		off := h.StringIDsOff + i*4
		if int(off)+4 > len(data) {
			return fmt.Errorf("dex: string_ids entry %d out of range", i)
		}
		so := binary.LittleEndian.Uint32(data[off:])
		f.StringOffsets[i] = so
		if int(so) >= len(data) {
			return fmt.Errorf("dex: string_data offset %d out of range", so)
		}
		r := &errReader{b: data, off: int(so)}
		r.uleb128() // utf16 length, not needed for decoding
		start := r.off
		for r.off < len(data) && data[r.off] != 0 {
			r.off++
		}
		f.Strings[i] = decodeMUTF8(data[start:r.off])
	}
	return nil
}

func (f *File) str(idx uint32) string {
	if int(idx) < len(f.Strings) {
		return f.Strings[idx]
	}
	return ""
}

func (f *File) typeDesc(idx uint32) string {
	if int(idx) < len(f.TypeDescs) {
		return f.TypeDescs[idx]
	}
	return ""
}

func (f *File) parseTypes(data []byte) error {
	h := f.Header
	f.TypeDescs = make([]string, h.TypeCount)
	f.TypeStringIdx = make([]uint32, h.TypeCount)
	for i := uint32(0); i < h.TypeCount; i++ {
		off := h.TypeIDsOff + i*4
		if int(off)+4 > len(data) {
			return fmt.Errorf("dex: type_ids entry %d out of range", i)
		}
		idx := binary.LittleEndian.Uint32(data[off:])
		f.TypeStringIdx[i] = idx
		f.TypeDescs[i] = f.str(idx)
	}
	return nil
}

func (f *File) parseProtos(data []byte) error {
	h := f.Header
	f.Protos = make([]Proto, h.ProtoCount)
	for i := uint32(0); i < h.ProtoCount; i++ {
		base := int(h.ProtoIDsOff + i*12)
		if base+12 > len(data) {
			return fmt.Errorf("dex: proto_ids entry %d out of range", i)
		}
		p := Proto{
			Index:         i,
			ShortyIdx:     binary.LittleEndian.Uint32(data[base:]),
			ReturnTypeIdx: binary.LittleEndian.Uint32(data[base+4:]),
			ParametersOff: binary.LittleEndian.Uint32(data[base+8:]),
		}
		p.Shorty = f.str(p.ShortyIdx)
		p.ReturnDesc = f.typeDesc(p.ReturnTypeIdx)
		if p.ParametersOff != 0 {
			r := &errReader{b: data, off: int(p.ParametersOff)}
			n := r.u4()
			if r.err == nil && int(p.ParametersOff)+4+int(n)*2 <= len(data) {
				p.ParamTypeIdx = make([]uint16, n)
				for j := uint32(0); j < n; j++ {
					p.ParamTypeIdx[j] = r.u2()
				}
				p.ParamDescs = make([]string, n)
				for j := range p.ParamTypeIdx {
					p.ParamDescs[j] = f.typeDesc(uint32(p.ParamTypeIdx[j]))
				}
			} else if r.err != nil {
				f.Warnings = append(f.Warnings, fmt.Sprintf("proto %d: %v", i, r.err))
			}
		}
		f.Protos[i] = p
	}
	return nil
}

func (f *File) parseFields(data []byte) error {
	h := f.Header
	f.Fields = make([]FieldID, h.FieldCount)
	for i := uint32(0); i < h.FieldCount; i++ {
		base := int(h.FieldIDsOff + i*8)
		if base+8 > len(data) {
			return fmt.Errorf("dex: field_ids entry %d out of range", i)
		}
		fd := FieldID{
			Index:    i,
			ClassIdx: binary.LittleEndian.Uint16(data[base:]),
			TypeIdx:  binary.LittleEndian.Uint16(data[base+2:]),
			NameIdx:  binary.LittleEndian.Uint32(data[base+4:]),
		}
		fd.ClassDesc = f.typeDesc(uint32(fd.ClassIdx))
		fd.TypeDesc = f.typeDesc(uint32(fd.TypeIdx))
		fd.Name = f.str(fd.NameIdx)
		f.Fields[i] = fd
	}
	return nil
}

func (f *File) parseMethods(data []byte) error {
	h := f.Header
	f.Methods = make([]MethodID, h.MethodCount)
	for i := uint32(0); i < h.MethodCount; i++ {
		base := int(h.MethodIDsOff + i*8)
		if base+8 > len(data) {
			return fmt.Errorf("dex: method_ids entry %d out of range", i)
		}
		m := MethodID{
			Index:    i,
			ClassIdx: binary.LittleEndian.Uint16(data[base:]),
			ProtoIdx: binary.LittleEndian.Uint16(data[base+2:]),
			NameIdx:  binary.LittleEndian.Uint32(data[base+4:]),
		}
		m.ClassDesc = f.typeDesc(uint32(m.ClassIdx))
		m.Name = f.str(m.NameIdx)
		if int(m.ProtoIdx) < len(f.Protos) {
			p := f.Protos[m.ProtoIdx]
			m.ReturnDesc = p.ReturnDesc
			m.ParamDescs = p.ParamDescs
		}
		var sb strings.Builder
		sb.WriteString(m.ClassDesc)
		sb.WriteString("->")
		sb.WriteString(m.Name)
		sb.WriteByte('(')
		sb.WriteString(strings.Join(m.ParamDescs, ""))
		sb.WriteByte(')')
		sb.WriteString(m.ReturnDesc)
		m.Descriptor = sb.String()
		f.Methods[i] = m
	}
	return nil
}

func (f *File) parseMap(data []byte) error {
	h := f.Header
	if h.MapOff == 0 || int(h.MapOff)+4 > len(data) {
		return nil
	}
	r := &errReader{b: data, off: int(h.MapOff)}
	n := r.u4()
	if r.err != nil {
		return r.err
	}
	if int(h.MapOff)+4+int(n)*12 > len(data) {
		return fmt.Errorf("dex: map_list out of range")
	}
	f.MapItems = make([]MapItem, 0, n)
	for i := uint32(0); i < n; i++ {
		f.MapItems = append(f.MapItems, MapItem{
			TypeCode: r.u2(),
			Size:     r.u4(),
			Offset:   r.u4(),
		})
	}
	sort.Slice(f.MapItems, func(i, j int) bool { return f.MapItems[i].Offset < f.MapItems[j].Offset })
	return nil
}

func (f *File) parseClassDefs(data []byte) error {
	h := f.Header
	f.Classes = make([]ClassDef, h.ClassCount)
	for i := uint32(0); i < h.ClassCount; i++ {
		base := int(h.ClassDefsOff + i*32)
		if base+32 > len(data) {
			return fmt.Errorf("dex: class_defs entry %d out of range", i)
		}
		c := ClassDef{
			Index:           i,
			ClassIdx:        binary.LittleEndian.Uint32(data[base:]),
			AccessFlags:     binary.LittleEndian.Uint32(data[base+4:]),
			SuperclassIdx:   binary.LittleEndian.Uint32(data[base+8:]),
			InterfacesOff:   binary.LittleEndian.Uint32(data[base+12:]),
			SourceFileIdx:   binary.LittleEndian.Uint32(data[base+16:]),
			AnnotationsOff:  binary.LittleEndian.Uint32(data[base+20:]),
			ClassDataOff:    binary.LittleEndian.Uint32(data[base+24:]),
			StaticValuesOff: binary.LittleEndian.Uint32(data[base+28:]),
		}
		c.Descriptor = f.typeDesc(c.ClassIdx)
		if c.SuperclassIdx != 0xffffffff {
			c.HasSuperclass = true
			c.SuperDescriptor = f.typeDesc(c.SuperclassIdx)
		}
		if c.SourceFileIdx != 0xffffffff {
			c.HasSourceFile = true
			c.SourceFile = f.str(c.SourceFileIdx)
		}
		if c.InterfacesOff != 0 {
			r := &errReader{b: data, off: int(c.InterfacesOff)}
			n := r.u4()
			if r.err == nil && int(c.InterfacesOff)+4+int(n)*2 <= len(data) {
				c.Interfaces = make([]uint16, n)
				c.InterfaceDescs = make([]string, n)
				for j := uint32(0); j < n; j++ {
					c.Interfaces[j] = r.u2()
					c.InterfaceDescs[j] = f.typeDesc(uint32(c.Interfaces[j]))
				}
			}
		}
		if c.ClassDataOff != 0 {
			cd, err := f.parseClassData(data, c.ClassDataOff)
			if err != nil {
				f.Warnings = append(f.Warnings, fmt.Sprintf("class %s: %v", c.Descriptor, err))
			} else {
				c.ClassData = cd
			}
		}
		if c.StaticValuesOff != 0 {
			vals, nodes, end, err := f.parseEncodedArray(data, c.StaticValuesOff)
			if err != nil {
				f.Warnings = append(f.Warnings, fmt.Sprintf("class %s static values: %v", c.Descriptor, err))
			} else {
				c.StaticValues = vals
				f.EncodedArrays = append(f.EncodedArrays, &EncodedArray{
					Index:     uint32(len(f.EncodedArrays)),
					Off:       c.StaticValuesOff,
					EndOff:    end,
					OwnerKind: "class_static_values",
					OwnerIdx:  c.ClassIdx,
					Values:    vals,
					All:       nodes,
				})
			}
		}
		f.Classes[i] = c
	}
	return nil
}

func (f *File) parseClassData(data []byte, off uint32) (*ClassData, error) {
	if int(off) >= len(data) {
		return nil, fmt.Errorf("class_data offset out of range")
	}
	r := &errReader{b: data, off: int(off)}
	cd := &ClassData{Off: off}
	ns := r.uleb128()
	ni := r.uleb128()
	nd := r.uleb128()
	nv := r.uleb128()
	if r.err != nil {
		return nil, r.err
	}
	readFields := func(n uint32, kind string) []EncodedField {
		out := make([]EncodedField, 0, n)
		var idx uint32
		for i := uint32(0); i < n; i++ {
			diff := r.uleb128()
			flags := r.uleb128()
			idx += diff
			out = append(out, EncodedField{FieldIdx: idx, AccessFlags: flags, Kind: kind})
			if r.err != nil {
				return out
			}
		}
		return out
	}
	cd.StaticFields = readFields(ns, "static")
	cd.InstanceFields = readFields(ni, "instance")
	readMethods := func(n uint32, kind string) []EncodedMethod {
		out := make([]EncodedMethod, 0, n)
		var idx uint32
		for i := uint32(0); i < n; i++ {
			diff := r.uleb128()
			flags := r.uleb128()
			codeOff := r.uleb128()
			idx += diff
			out = append(out, EncodedMethod{MethodIdx: idx, AccessFlags: flags, CodeOff: codeOff, Kind: kind})
			if r.err != nil {
				return out
			}
		}
		return out
	}
	cd.DirectMethods = readMethods(nd, "direct")
	cd.VirtualMethods = readMethods(nv, "virtual")
	cd.EndOff = uint32(r.off)
	if r.err != nil {
		return nil, r.err
	}
	return cd, nil
}

func (f *File) sortClassData() {
	// Parse every code_item exactly once, keyed by method index.
	for i := range f.Classes {
		c := &f.Classes[i]
		if c.ClassData == nil {
			continue
		}
		for _, m := range append(append([]EncodedMethod{}, c.ClassData.DirectMethods...), c.ClassData.VirtualMethods...) {
			if m.CodeOff == 0 || f.CodeItems[m.MethodIdx] != nil {
				continue
			}
			ci, err := f.parseCodeItem(m.MethodIdx, m.CodeOff)
			if err != nil {
				f.Warnings = append(f.Warnings, fmt.Sprintf("method %d code: %v", m.MethodIdx, err))
				continue
			}
			f.CodeItems[m.MethodIdx] = ci
			f.parseDebugFor(ci, f.debugPositions)
		}
	}
}

func (f *File) parseMethodHandles(data []byte) error {
	var size, off uint32
	for _, m := range f.MapItems {
		if m.TypeCode == 0x0008 {
			size, off = m.Size, m.Offset
		}
	}
	if size == 0 || off == 0 {
		return nil
	}
	f.MethodHandles = make([]MethodHandle, 0, size)
	for i := uint32(0); i < size; i++ {
		base := int(off + i*8)
		if base+8 > len(data) {
			return fmt.Errorf("dex: method_handles out of range")
		}
		f.MethodHandles = append(f.MethodHandles, MethodHandle{
			Index:     i,
			Type:      uint32(binary.LittleEndian.Uint16(data[base:])),
			MemberIdx: uint32(binary.LittleEndian.Uint16(data[base+4:])),
		})
	}
	return nil
}

func (f *File) parseCallSites(data []byte) error {
	var size, off uint32
	for _, m := range f.MapItems {
		if m.TypeCode == 0x0007 {
			size, off = m.Size, m.Offset
		}
	}
	if size == 0 || off == 0 {
		return nil
	}
	f.CallSites = make([]CallSite, 0, size)
	for i := uint32(0); i < size; i++ {
		base := int(off + i*4)
		if base+4 > len(data) {
			return fmt.Errorf("dex: call_sites out of range")
		}
		csOff := binary.LittleEndian.Uint32(data[base:])
		vals, nodes, end, err := f.parseEncodedArray(data, csOff)
		if err != nil {
			f.Warnings = append(f.Warnings, fmt.Sprintf("call site %d: %v", i, err))
			continue
		}
		cs := CallSite{Index: i, Off: csOff, ValueCount: uint32(len(vals)), Args: vals}
		if len(vals) >= 3 {
			if vals[0].HasIdx && vals[0].Kind == "method_handle" {
				cs.MethodHandleIdx, cs.HasHandle = vals[0].ValueIdx, true
			}
			if vals[1].HasIdx && vals[1].Kind == "string" {
				cs.NameIdx, cs.HasName = vals[1].ValueIdx, true
			}
			if vals[2].HasIdx && vals[2].Kind == "method_type" {
				cs.ProtoIdx, cs.HasProto = vals[2].ValueIdx, true
			}
		}
		f.CallSites = append(f.CallSites, cs)
		f.EncodedArrays = append(f.EncodedArrays, &EncodedArray{
			Index:     uint32(len(f.EncodedArrays)),
			Off:       csOff,
			EndOff:    end,
			OwnerKind: "call_site",
			OwnerIdx:  i,
			Values:    vals,
			All:       nodes,
		})
	}
	return nil
}
