package dex

import (
	"encoding/binary"
	"fmt"
	"math"
)

var valueKindNames = map[byte]string{
	0x00: "byte", 0x02: "short", 0x03: "char", 0x04: "int", 0x06: "long",
	0x10: "float", 0x11: "double", 0x15: "method_type", 0x16: "method_handle",
	0x17: "string", 0x18: "type", 0x19: "field", 0x1a: "method", 0x1b: "enum",
	0x1c: "array", 0x1d: "annotation", 0x1e: "null", 0x1f: "boolean",
}

// parseEncodedValue decodes one encoded_value, appending the node and every
// nested child to out (pre-order). depth/ordinal describe the position, and
// hasName/nameIdx are meaningful for annotation elements only.
func (f *File) parseEncodedValue(data []byte, off int, depth, ordinal uint32, nameIdx uint32, hasName bool, out *[]AnnotationElement) (EncodedValue, int, error) {
	if off >= len(data) {
		return EncodedValue{}, off, fmt.Errorf("encoded_value offset %d out of range", off)
	}
	start := off
	header := data[off]
	off++
	typ := header & 0x1f
	arg := int(header >> 5)

	v := EncodedValue{Off: uint32(start), Kind: valueKindNames[typ]}
	idx := uint32(0)
	var i64 int64
	var f64 float64
	hasFloat := false
	var end int

	readUInt := func(n int) uint64 {
		var x uint64
		for i := 0; i < n; i++ {
			if off+i < len(data) {
				x |= uint64(data[off+i]) << (8 * i)
			}
		}
		return x
	}
	signExtend := func(x uint64, bytes int) int64 {
		shift := uint(64 - bytes*8)
		return int64(x<<shift) >> shift
	}

	switch typ {
	case 0x1e: // null
		v.HasIdx = false
		end = off
	case 0x1f: // boolean
		i64 = int64(header >> 5)
		end = off
	case 0x00, 0x02, 0x03, 0x04, 0x06:
		n := arg + 1
		x := readUInt(n)
		off += n
		switch typ {
		case 0x00:
			i64 = int64(int8(uint8(x)))
		case 0x02:
			i64 = signExtend(x, n)
		case 0x03:
			i64 = int64(uint16(x))
		case 0x04:
			i64 = signExtend(x, n)
		case 0x06:
			i64 = signExtend(x, n)
		}
		end = off
	case 0x10: // float: right-zero-extended to 32 bits
		x := uint32(readUInt(minInt(arg+1, 4)))
		off += arg + 1
		if arg < 3 {
			x <<= uint(8 * (3 - arg))
		}
		f64 = float64(math.Float32frombits(x))
		i64 = int64(x)
		hasFloat = true
		end = off
	case 0x11: // double: right-zero-extended to 64 bits
		x := readUInt(arg + 1)
		off += arg + 1
		if arg < 7 {
			x <<= uint(8 * (7 - arg))
		}
		f64 = math.Float64frombits(x)
		i64 = int64(x)
		hasFloat = true
		end = off
	case 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b:
		n := arg + 1
		idx = uint32(readUInt(n))
		off += n
		end = off
	case 0x1c: // array
		n := int(readULEBAt(data, &off))
		for i := 0; i < n; i++ {
			_, next, err := f.parseEncodedValue(data, off, depth+1, uint32(i), 0, false, out)
			if err != nil {
				return v, end, err
			}
			off = next
		}
		end = off
	case 0x1d: // nested annotation
		idx = readULEBAt(data, &off)
		n := int(readULEBAt(data, &off))
		for i := 0; i < n; i++ {
			nidx := readULEBAt(data, &off)
			_, next, err := f.parseEncodedValue(data, off, depth+1, uint32(i), nidx, true, out)
			if err != nil {
				return v, end, err
			}
			off = next
		}
		end = off
	default:
		return v, off, fmt.Errorf("unknown encoded_value type 0x%02x", typ)
	}

	v.I64 = i64
	v.F64 = f64
	v.HasFloat = hasFloat
	v.EndOff = uint32(end)
	if typ == 0x15 || typ == 0x16 || typ == 0x17 || typ == 0x18 || typ == 0x19 || typ == 0x1a || typ == 0x1b {
		v.ValueIdx = idx
		v.HasIdx = true
	}
	if typ == 0x1d {
		v.ValueIdx = idx
		v.HasIdx = true
	}
	*out = append(*out, AnnotationElement{
		Depth:       depth,
		Ordinal:     ordinal,
		NameIdx:     nameIdx,
		HasName:     hasName,
		Name:        f.str(nameIdx),
		ValueOff:    uint32(start),
		ValueEndOff: uint32(end),
		Value:       v,
	})
	return v, end, nil
}

func readULEBAt(data []byte, off *int) uint32 {
	var result uint32
	var shift uint
	for i := 0; i < 5 && *off < len(data); i++ {
		b := data[*off]
		*off = *off + 1
		result |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return result
		}
		shift += 7
	}
	return result
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func readU32At(data []byte, off uint32) (uint32, bool) {
	if int(off)+4 > len(data) {
		return 0, false
	}
	return binary.LittleEndian.Uint32(data[off:]), true
}

// parseEncodedArray decodes an encoded_array_item located at off. It returns
// the top-level values, every flattened node (for encoded_array_values) and
// the offset one past the array.
func (f *File) parseEncodedArray(data []byte, off uint32) ([]EncodedValue, []AnnotationElement, uint32, error) {
	if int(off) >= len(data) {
		return nil, nil, off, fmt.Errorf("encoded_array offset out of range")
	}
	pos := int(off)
	n := int(readULEBAt(data, &pos))
	vals := make([]EncodedValue, 0, n)
	var all []AnnotationElement
	for i := 0; i < n; i++ {
		v, next, err := f.parseEncodedValue(data, pos, 0, uint32(i), 0, false, &all)
		if err != nil {
			return vals, all, off, err
		}
		pos = next
		vals = append(vals, v)
	}
	return vals, all, uint32(pos), nil
}

// parseAnnotationItem decodes one annotation_item at off.
func (f *File) parseAnnotationItem(data []byte, off uint32) (*Annotation, error) {
	if int(off) >= len(data) {
		return nil, fmt.Errorf("annotation offset out of range")
	}
	pos := int(off)
	vis := uint32(data[pos])
	pos++
	typeIdx := readULEBAt(data, &pos)
	n := int(readULEBAt(data, &pos))
	a := &Annotation{
		Off:          off,
		Visibility:   vis,
		TypeIdx:      typeIdx,
		TypeDesc:     f.typeDesc(typeIdx),
		ElementCount: uint32(n),
	}
	for i := 0; i < n; i++ {
		nameIdx := readULEBAt(data, &pos)
		var nodes []AnnotationElement
		_, next, err := f.parseEncodedValue(data, pos, 0, uint32(i), nameIdx, true, &nodes)
		if err != nil {
			return nil, err
		}
		pos = next
		a.Elements = append(a.Elements, nodes...)
	}
	return a, nil
}

// parseAnnotationSet decodes an annotation_set_item, caching by offset.
func (f *File) parseAnnotationSet(data []byte, off uint32, cache map[uint32]*AnnotationSet, items map[uint32]*Annotation) *AnnotationSet {
	if s, ok := cache[off]; ok {
		return s
	}
	size, ok := readU32At(data, off)
	if !ok || int(off)+4+int(size)*4 > len(data) {
		return nil
	}
	s := &AnnotationSet{Off: off, Size: size}
	for i := uint32(0); i < size; i++ {
		aoff := binary.LittleEndian.Uint32(data[off+4+i*4:])
		s.Elements = append(s.Elements, aoff)
		if _, seen := items[aoff]; !seen {
			if a, err := f.parseAnnotationItem(data, aoff); err == nil {
				items[aoff] = a
			}
		}
	}
	cache[off] = s
	f.AnnotationSets = append(f.AnnotationSets, s)
	return s
}

// parseAnnotations walks every class annotations directory.
func (f *File) parseAnnotations(data []byte) error {
	setCache := map[uint32]*AnnotationSet{}
	itemCache := map[uint32]*Annotation{}
	refCache := map[uint32]*AnnotationRefList{}

	ensureItems := func() []*Annotation {
		out := make([]*Annotation, 0, len(itemCache))
		for _, a := range itemCache {
			out = append(out, a)
		}
		return out
	}

	for i := range f.Classes {
		c := &f.Classes[i]
		if c.AnnotationsOff == 0 || int(c.AnnotationsOff)+16 > len(data) {
			continue
		}
		base := int(c.AnnotationsOff)
		if base+20 > len(data) {
			continue
		}
		d := &AnnotationDirectory{AnnotationsOff: c.AnnotationsOff}
		d.ClassIdx = binary.LittleEndian.Uint32(data[base:])
		classSetOff := binary.LittleEndian.Uint32(data[base+4:])
		fieldsSize := binary.LittleEndian.Uint32(data[base+8:])
		methodsSize := binary.LittleEndian.Uint32(data[base+12:])
		paramsSize := binary.LittleEndian.Uint32(data[base+16:])
		pos := base + 20
		if fieldsSize+methodsSize+paramsSize > uint32(len(data)) {
			f.Warnings = append(f.Warnings, fmt.Sprintf("class %s: implausible annotation directory", c.Descriptor))
			continue
		}
		for j := uint32(0); j < fieldsSize; j++ {
			if pos+8 > len(data) {
				break
			}
			e := AnnotationDirectoryEntry{Kind: "field",
				EntryIdx:         binary.LittleEndian.Uint32(data[pos:]),
				AnnotationSetOff: binary.LittleEndian.Uint32(data[pos+4:])}
			d.Fields = append(d.Fields, e)
			f.parseAnnotationSet(data, e.AnnotationSetOff, setCache, itemCache)
			pos += 8
		}
		for j := uint32(0); j < methodsSize; j++ {
			if pos+8 > len(data) {
				break
			}
			e := AnnotationDirectoryEntry{Kind: "method",
				EntryIdx:         binary.LittleEndian.Uint32(data[pos:]),
				AnnotationSetOff: binary.LittleEndian.Uint32(data[pos+4:])}
			d.Methods = append(d.Methods, e)
			f.parseAnnotationSet(data, e.AnnotationSetOff, setCache, itemCache)
			pos += 8
		}
		for j := uint32(0); j < paramsSize; j++ {
			if pos+8 > len(data) {
				break
			}
			e := AnnotationDirectoryEntry{Kind: "parameter",
				EntryIdx:   binary.LittleEndian.Uint32(data[pos:]),
				RefListOff: binary.LittleEndian.Uint32(data[pos+4:])}
			d.Parameters = append(d.Parameters, e)
			pos += 8
			rl := f.parseAnnotationRefList(data, e.RefListOff, setCache, itemCache, refCache)
			if rl != nil {
				e.AnnotationSetOff = 0
			}
		}
		if classSetOff != 0 {
			if s := f.parseAnnotationSet(data, classSetOff, setCache, itemCache); s != nil {
				d.ClassSetOff = classSetOff
				d.HasClassSet = true
			}
		}
		f.AnnotationDirs = append(f.AnnotationDirs, d)
	}

	f.Annotations = ensureItems()
	return nil
}

func (f *File) parseAnnotationRefList(data []byte, off uint32, sets map[uint32]*AnnotationSet, items map[uint32]*Annotation, cache map[uint32]*AnnotationRefList) *AnnotationRefList {
	if off == 0 {
		return nil
	}
	if rl, ok := cache[off]; ok {
		return rl
	}
	size, ok := readU32At(data, off)
	if !ok || int(off)+4+int(size)*4 > len(data) {
		return nil
	}
	rl := &AnnotationRefList{Off: off}
	for i := uint32(0); i < size; i++ {
		soff := binary.LittleEndian.Uint32(data[off+4+i*4:])
		rl.Sets = append(rl.Sets, soff)
		if soff != 0 {
			rl.HasSet = append(rl.HasSet, true)
			f.parseAnnotationSet(data, soff, sets, items)
		} else {
			rl.HasSet = append(rl.HasSet, false)
		}
	}
	cache[off] = rl
	f.AnnotationRefLists = append(f.AnnotationRefLists, rl)
	return rl
}
