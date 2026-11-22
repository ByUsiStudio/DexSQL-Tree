// Package dextest builds small but structurally valid DEX images for tests and
// for the bundled demo. It only emits 035 images with a single class.
package dextest

import (
	"crypto/sha1"
	"encoding/binary"
	"hash/adler32"
	"sort"
)

// Method is a method definition with raw Dalvik code units.
type Method struct {
	MethodIdx uint32
	Access    uint32
	Registers uint16
	Ins       uint16
	Outs      uint16
	Insns     []uint16
	Debug     *Debug
}

// Debug optionally attaches a hand-written debug_info_item.
type Debug struct {
	LineStart uint32
	Params    []uint32 // 0xffffffff for none
	Bytes     []byte   // state machine, must end with 0x00
}

// Field is a field definition.
type Field struct {
	FieldIdx uint32
	Access   uint32
}

// Class describes one class definition.
type Class struct {
	Descriptor   string
	Super        string
	Interfaces   []string
	SourceFile   string
	Access       uint32
	StaticFields []Field
	InstFields   []Field
	Direct       []Method
	Virtual      []Method
}

// Builder accumulates symbols and resolves indices by deduplication.
type Builder struct {
	strings   []string
	strIdx    map[string]uint32
	types     []uint32
	typeIdx   map[string]uint16
	protos    []protoDef
	protoIdx  map[string]uint16
	fields    []fieldDef
	fieldIdx  map[string]uint32
	methods   []methodDef
	methodIdx map[string]uint32
	classes   []Class
}

type methodKey struct{ class, method uint32 }

type protoDef struct {
	shortyIdx uint32
	retIdx    uint16
	params    []uint16
}

type fieldDef struct {
	classIdx uint16
	typeIdx  uint16
	nameIdx  uint32
}

type methodDef struct {
	classIdx uint16
	protoIdx uint16
	nameIdx  uint32
}

// New returns an empty builder.
func New() *Builder {
	return &Builder{
		strIdx:    map[string]uint32{},
		typeIdx:   map[string]uint16{},
		protoIdx:  map[string]uint16{},
		fieldIdx:  map[string]uint32{},
		methodIdx: map[string]uint32{},
	}
}

// Str interns a string.
func (b *Builder) Str(s string) uint32 {
	if i, ok := b.strIdx[s]; ok {
		return i
	}
	i := uint32(len(b.strings))
	b.strings = append(b.strings, s)
	b.strIdx[s] = i
	return i
}

// Type interns a type descriptor.
func (b *Builder) Type(desc string) uint16 {
	if i, ok := b.typeIdx[desc]; ok {
		return i
	}
	i := uint16(len(b.types))
	b.types = append(b.types, b.Str(desc))
	b.typeIdx[desc] = i
	return i
}

// Proto interns a method prototype.
func (b *Builder) Proto(ret string, params ...string) uint16 {
	key := ret + "|"
	for _, p := range params {
		key += p + ";"
	}
	if i, ok := b.protoIdx[key]; ok {
		return i
	}
	p := protoDef{retIdx: b.Type(ret)}
	for _, d := range params {
		p.params = append(p.params, b.Type(d))
	}
	shorty := string([]byte{shortyChar(ret)})
	for _, d := range params {
		shorty += string([]byte{shortyChar(d)})
	}
	p.shortyIdx = b.Str(shorty)
	i := uint16(len(b.protos))
	b.protos = append(b.protos, p)
	b.protoIdx[key] = i
	return i
}

func shortyChar(d string) byte {
	if d == "" {
		return 'V'
	}
	if d[0] == 'L' || d[0] == '[' {
		return 'L'
	}
	return d[0]
}

// Field interns a field id.
func (b *Builder) Field(classDesc, name, typeDesc string) uint32 {
	key := classDesc + "->" + name + ":" + typeDesc
	if i, ok := b.fieldIdx[key]; ok {
		return i
	}
	i := uint32(len(b.fields))
	b.fields = append(b.fields, fieldDef{classIdx: b.Type(classDesc), typeIdx: b.Type(typeDesc), nameIdx: b.Str(name)})
	b.fieldIdx[key] = i
	return i
}

// Method interns a method id.
func (b *Builder) Method(classDesc, name, ret string, params ...string) uint32 {
	key := classDesc + "->" + name + "(" + join(params) + ")" + ret
	if i, ok := b.methodIdx[key]; ok {
		return i
	}
	i := uint32(len(b.methods))
	b.methods = append(b.methods, methodDef{classIdx: b.Type(classDesc), protoIdx: b.Proto(ret, params...), nameIdx: b.Str(name)})
	b.methodIdx[key] = i
	return i
}

// Class registers a class definition, interning its descriptors immediately so
// section sizes are final before Build computes offsets.
func (b *Builder) Class(c Class) {
	b.Type(c.Descriptor)
	if c.Super != "" {
		b.Type(c.Super)
	}
	for _, i := range c.Interfaces {
		b.Type(i)
	}
	if c.SourceFile != "" {
		b.Str(c.SourceFile)
	}
	b.classes = append(b.classes, c)
}

func join(ss []string) string {
	out := ""
	for _, s := range ss {
		out += s
	}
	return out
}

func uleb(v uint32) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func align4(n int) int { return (n + 3) &^ 3 }

// Build serialises the dex image.
func (b *Builder) Build() []byte {
	ns, nt, np, nf, nm, nc := len(b.strings), len(b.types), len(b.protos), len(b.fields), len(b.methods), len(b.classes)
	headerSize := 112
	stringIDsOff := headerSize
	typeIDsOff := stringIDsOff + ns*4
	protoIDsOff := typeIDsOff + nt*4
	fieldIDsOff := protoIDsOff + np*12
	methodIDsOff := fieldIDsOff + nf*8
	classDefsOff := methodIDsOff + nm*8
	dataOff := align4(classDefsOff + nc*32)

	var data []byte
	pos := dataOff
	padTo := func() {
		for pos%4 != 0 {
			data = append(data, 0)
			pos++
		}
	}
	put := func(bs []byte) int {
		off := pos
		data = append(data, bs...)
		pos += len(bs)
		return off
	}

	// string_data_item
	stringDataOff := make([]int, ns)
	for i, s := range b.strings {
		// MUTF-8 encoding: ASCII in tests; encode U+0000 as C0 80 just in case.
		var enc []byte
		for _, r := range s {
			switch {
			case r == 0:
				enc = append(enc, 0xc0, 0x80)
			case r < 0x80:
				enc = append(enc, byte(r))
			case r < 0x800:
				enc = append(enc, 0xc0|byte(r>>6), 0x80|byte(r&0x3f))
			case r < 0x10000:
				enc = append(enc, 0xe0|byte(r>>12), 0x80|byte((r>>6)&0x3f), 0x80|byte(r&0x3f))
			default:
				r -= 0x10000
				hi := 0xd800 + (r >> 10)
				lo := 0xdc00 + (r & 0x3ff)
				for _, x := range []rune{hi, lo} {
					enc = append(enc, 0xe0|byte(x>>12), 0x80|byte((x>>6)&0x3f), 0x80|byte(x&0x3f))
				}
			}
		}
		buf := append(uleb(uint32(len([]rune(s)))), enc...)
		buf = append(buf, 0)
		stringDataOff[i] = put(buf)
	}

	// type_list for each proto's parameters
	typeListOff := make([]int, np)
	for i, p := range b.protos {
		if len(p.params) == 0 {
			typeListOff[i] = 0
			continue
		}
		padTo()
		buf := make([]byte, 4+len(p.params)*2)
		binary.LittleEndian.PutUint32(buf, uint32(len(p.params)))
		for j, t := range p.params {
			binary.LittleEndian.PutUint16(buf[4+j*2:], t)
		}
		typeListOff[i] = put(buf)
	}

	// code_item per method definition
	codeOff := map[methodKey]int{}
	for _, c := range b.classes {
		cidx := uint32(b.Type(c.Descriptor))
		for _, list := range [][]Method{c.Direct, c.Virtual} {
			for _, m := range list {
				padTo()
				hdr := make([]byte, 16)
				binary.LittleEndian.PutUint16(hdr[0:], m.Registers)
				binary.LittleEndian.PutUint16(hdr[2:], m.Ins)
				binary.LittleEndian.PutUint16(hdr[4:], m.Outs)
				binary.LittleEndian.PutUint16(hdr[6:], 0) // tries
				debugOff := 0
				if m.Debug != nil {
					// Debug info is emitted later; reserve via placeholder.
					debugOff = -1
				}
				binary.LittleEndian.PutUint32(hdr[8:], uint32(max0(debugOff)))
				binary.LittleEndian.PutUint32(hdr[12:], uint32(len(m.Insns)))
				buf := append(hdr, make([]byte, len(m.Insns)*2)...)
				for i, u := range m.Insns {
					binary.LittleEndian.PutUint16(buf[16+i*2:], u)
				}
				off := put(buf)
				codeOff[methodKey{cidx, m.MethodIdx}] = off
				if m.Debug != nil {
					dbg := uleb(m.Debug.LineStart)
					dbg = append(dbg, uleb(uint32(len(m.Debug.Params)))...)
					for _, p := range m.Debug.Params {
						dbg = append(dbg, uleb(p)...)
					}
					dbg = append(dbg, m.Debug.Bytes...)
					doff := put(dbg)
					binary.LittleEndian.PutUint32(data[off-dataOff+8:], uint32(doff))
				}
			}
		}
	}

	// class_data_item
	classDataOff := make([]int, nc)
	for i, c := range b.classes {
		if len(c.StaticFields)+len(c.InstFields)+len(c.Direct)+len(c.Virtual) == 0 {
			classDataOff[i] = 0
			continue
		}
		buf := append(uleb(uint32(len(c.StaticFields))), uleb(uint32(len(c.InstFields)))...)
		buf = append(buf, uleb(uint32(len(c.Direct)))...)
		buf = append(buf, uleb(uint32(len(c.Virtual)))...)
		encFields := func(fs []Field) {
			var prev uint32
			for _, f := range fs {
				buf = append(buf, uleb(f.FieldIdx-prev)...)
				buf = append(buf, uleb(f.Access)...)
				prev = f.FieldIdx
			}
		}
		encFields(c.StaticFields)
		encFields(c.InstFields)
		encMethods := func(ms []Method) {
			var prev uint32
			for _, m := range ms {
				buf = append(buf, uleb(m.MethodIdx-prev)...)
				buf = append(buf, uleb(m.Access)...)
				buf = append(buf, uleb(uint32(codeOff[methodKey{uint32(b.Type(c.Descriptor)), m.MethodIdx}]))...)
				prev = m.MethodIdx
			}
		}
		encMethods(c.Direct)
		encMethods(c.Virtual)
		classDataOff[i] = put(buf)
	}

	// map_list
	padTo()
	mapOff := pos
	items := []struct {
		typ  uint16
		size uint32
		off  uint32
	}{
		{0x0000, 1, 0},
		{0x0001, uint32(ns), uint32(stringIDsOff)},
		{0x0002, uint32(nt), uint32(typeIDsOff)},
		{0x0003, uint32(np), uint32(protoIDsOff)},
		{0x0004, uint32(nf), uint32(fieldIDsOff)},
		{0x0005, uint32(nm), uint32(methodIDsOff)},
		{0x0006, uint32(nc), uint32(classDefsOff)},
		{0x2002, uint32(ns), uint32(stringDataOff[0])},
	}
	if n := nonZero(typeListOff); n > 0 {
		items = append(items, struct {
			typ  uint16
			size uint32
			off  uint32
		}{0x1001, uint32(n), uint32(firstNonZero(typeListOff))})
	}
	if len(codeOff) > 0 {
		items = append(items, struct {
			typ  uint16
			size uint32
			off  uint32
		}{0x2001, uint32(len(codeOff)), uint32(firstInt(codeOff))})
	}
	if n := nonZero(classDataOff); n > 0 {
		items = append(items, struct {
			typ  uint16
			size uint32
			off  uint32
		}{0x2000, uint32(n), uint32(firstNonZero(classDataOff))})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].off < items[j].off })
	mbuf := make([]byte, 4+(len(items)+1)*12)
	binary.LittleEndian.PutUint32(mbuf, uint32(len(items)+1))
	off := 4
	for _, it := range items {
		binary.LittleEndian.PutUint16(mbuf[off:], it.typ)
		binary.LittleEndian.PutUint32(mbuf[off+4:], it.size)
		binary.LittleEndian.PutUint32(mbuf[off+8:], it.off)
		off += 12
	}
	binary.LittleEndian.PutUint16(mbuf[off:], 0x1000)
	binary.LittleEndian.PutUint32(mbuf[off+4:], 1)
	binary.LittleEndian.PutUint32(mbuf[off+8:], uint32(mapOff))
	put(mbuf)

	total := dataOff + len(data)
	out := make([]byte, total)
	copy(out[0:], []byte("dex\n035\x00"))
	binary.LittleEndian.PutUint32(out[32:], uint32(total))
	binary.LittleEndian.PutUint32(out[36:], 112)
	binary.LittleEndian.PutUint32(out[40:], 0x12345678)
	binary.LittleEndian.PutUint32(out[44:], 0) // link_size
	binary.LittleEndian.PutUint32(out[48:], 0) // link_off
	binary.LittleEndian.PutUint32(out[52:], uint32(mapOff))
	binary.LittleEndian.PutUint32(out[56:], uint32(ns))
	binary.LittleEndian.PutUint32(out[60:], uint32(stringIDsOff))
	binary.LittleEndian.PutUint32(out[64:], uint32(nt))
	binary.LittleEndian.PutUint32(out[68:], uint32(typeIDsOff))
	binary.LittleEndian.PutUint32(out[72:], uint32(np))
	binary.LittleEndian.PutUint32(out[76:], uint32(protoIDsOff))
	binary.LittleEndian.PutUint32(out[80:], uint32(nf))
	binary.LittleEndian.PutUint32(out[84:], uint32(fieldIDsOff))
	binary.LittleEndian.PutUint32(out[88:], uint32(nm))
	binary.LittleEndian.PutUint32(out[92:], uint32(methodIDsOff))
	binary.LittleEndian.PutUint32(out[96:], uint32(nc))
	binary.LittleEndian.PutUint32(out[100:], uint32(classDefsOff))
	binary.LittleEndian.PutUint32(out[104:], uint32(len(data)))
	binary.LittleEndian.PutUint32(out[108:], uint32(dataOff))

	for i := 0; i < ns; i++ {
		binary.LittleEndian.PutUint32(out[stringIDsOff+i*4:], uint32(stringDataOff[i]))
	}
	for i, d := range b.types {
		binary.LittleEndian.PutUint32(out[typeIDsOff+i*4:], d)
	}
	for i, p := range b.protos {
		base := protoIDsOff + i*12
		binary.LittleEndian.PutUint32(out[base:], p.shortyIdx)
		binary.LittleEndian.PutUint32(out[base+4:], uint32(p.retIdx))
		binary.LittleEndian.PutUint32(out[base+8:], uint32(typeListOff[i]))
	}
	for i, f := range b.fields {
		base := fieldIDsOff + i*8
		binary.LittleEndian.PutUint16(out[base:], f.classIdx)
		binary.LittleEndian.PutUint16(out[base+2:], f.typeIdx)
		binary.LittleEndian.PutUint32(out[base+4:], f.nameIdx)
	}
	for i, m := range b.methods {
		base := methodIDsOff + i*8
		binary.LittleEndian.PutUint16(out[base:], m.classIdx)
		binary.LittleEndian.PutUint16(out[base+2:], m.protoIdx)
		binary.LittleEndian.PutUint32(out[base+4:], m.nameIdx)
	}
	for i, c := range b.classes {
		base := classDefsOff + i*32
		binary.LittleEndian.PutUint32(out[base:], uint32(b.Type(c.Descriptor)))
		binary.LittleEndian.PutUint32(out[base+4:], c.Access)
		super := uint32(0xffffffff)
		if c.Super != "" {
			super = uint32(b.Type(c.Super))
		}
		binary.LittleEndian.PutUint32(out[base+8:], super)
		binary.LittleEndian.PutUint32(out[base+12:], 0) // interfaces
		src := uint32(0xffffffff)
		if c.SourceFile != "" {
			src = b.Str(c.SourceFile)
		}
		binary.LittleEndian.PutUint32(out[base+16:], src)
		binary.LittleEndian.PutUint32(out[base+20:], 0)
		binary.LittleEndian.PutUint32(out[base+24:], uint32(classDataOff[i]))
		binary.LittleEndian.PutUint32(out[base+28:], 0)
	}
	copy(out[dataOff:], data)

	// checksum + signature
	sum := adler32.Checksum(out[12:])
	binary.LittleEndian.PutUint32(out[8:], sum)
	sig := sha1.Sum(out[32:])
	copy(out[12:32], sig[:])
	return out
}

func nonZero(xs []int) int {
	n := 0
	for _, x := range xs {
		if x != 0 {
			n++
		}
	}
	return n
}

func firstNonZero(xs []int) int {
	for _, x := range xs {
		if x != 0 {
			return x
		}
	}
	return 0
}

func firstInt(m map[methodKey]int) int {
	best := int(^uint(0) >> 1)
	for _, v := range m {
		if v < best {
			best = v
		}
	}
	if best == int(^uint(0)>>1) {
		return 0
	}
	return best
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
