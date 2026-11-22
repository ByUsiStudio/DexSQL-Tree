package dex

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

// errReader is a small bounds-checked cursor over a DEX byte slice.
type errReader struct {
	b   []byte
	off int
	err error
}

func (r *errReader) fail(format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf(format, args...)
	}
}

func (r *errReader) need(n int) bool {
	if r.err != nil {
		return false
	}
	if n < 0 || r.off+n > len(r.b) {
		r.fail("unexpected end of dex at offset %d (need %d bytes)", r.off, n)
		return false
	}
	return true
}

func (r *errReader) u1() uint8 {
	if !r.need(1) {
		return 0
	}
	v := r.b[r.off]
	r.off++
	return v
}

func (r *errReader) u2() uint16 {
	if !r.need(2) {
		return 0
	}
	v := binary.LittleEndian.Uint16(r.b[r.off:])
	r.off += 2
	return v
}

func (r *errReader) u4() uint32 {
	if !r.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b[r.off:])
	r.off += 4
	return v
}

func (r *errReader) u8() uint64 {
	if !r.need(8) {
		return 0
	}
	v := binary.LittleEndian.Uint64(r.b[r.off:])
	r.off += 8
	return v
}

func (r *errReader) bytes(n int) []byte {
	if !r.need(n) {
		return nil
	}
	v := r.b[r.off : r.off+n]
	r.off += n
	return v
}

// uleb128 reads an unsigned LEB128 value of at most 5 bytes.
func (r *errReader) uleb128() uint32 {
	var result uint32
	var shift uint
	for i := 0; i < 5; i++ {
		b := r.u1()
		if r.err != nil {
			return 0
		}
		result |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return result
		}
		shift += 7
	}
	r.fail("malformed uleb128 at offset %d", r.off)
	return 0
}

// sleb128 reads a signed LEB128 value.
func (r *errReader) sleb128() int32 {
	var result int32
	var shift uint
	var b uint8
	for i := 0; i < 5; i++ {
		b = r.u1()
		if r.err != nil {
			return 0
		}
		result |= int32(b&0x7f) << shift
		shift += 7
		if b&0x80 == 0 {
			break
		}
	}
	if shift < 32 && b&0x40 != 0 {
		result |= -1 << shift
	}
	return result
}

// sleb128_64 reads a signed LEB128 value used by 64-bit constants.
func (r *errReader) sleb128_64() int64 {
	var result int64
	var shift uint
	var b uint8
	for i := 0; i < 10; i++ {
		b = r.u1()
		if r.err != nil {
			return 0
		}
		result |= int64(b&0x7f) << shift
		shift += 7
		if b&0x80 == 0 {
			break
		}
	}
	if shift < 64 && b&0x40 != 0 {
		result |= -1 << shift
	}
	return result
}

// decodeMUTF8 converts Modified UTF-8 (as used by DEX string_data_item) to a
// Go string. MUTF-8 differs from UTF-8 by encoding U+0000 as C0 80 and by
// using CESU-8 style surrogate pairs for supplementary characters.
func decodeMUTF8(b []byte) string {
	out := make([]rune, 0, len(b))
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0:
			return string(out)
		case c < 0x80:
			out = append(out, rune(c))
			i++
		case c&0xe0 == 0xc0:
			if i+1 >= len(b) {
				return string(out)
			}
			out = append(out, rune(c&0x1f)<<6|rune(b[i+1]&0x3f))
			i += 2
		case c&0xf0 == 0xe0:
			if i+2 >= len(b) {
				return string(out)
			}
			r := rune(c&0x0f)<<12 | rune(b[i+1]&0x3f)<<6 | rune(b[i+2]&0x3f)
			i += 3
			if r >= 0xd800 && r <= 0xdbff && i+2 < len(b) && b[i]&0xf0 == 0xe0 {
				lo := rune(b[i]&0x0f)<<12 | rune(b[i+1]&0x3f)<<6 | rune(b[i+2]&0x3f)
				if lo >= 0xdc00 && lo <= 0xdfff {
					r = 0x10000 + (r-0xd800)<<10 + (lo - 0xdc00)
					i += 3
				}
			}
			if r >= 0xd800 && r <= 0xdfff {
				r = utf8.RuneError
			}
			out = append(out, r)
		default:
			out = append(out, utf8.RuneError)
			i++
		}
	}
	return string(out)
}

// JavaName renders a DEX type descriptor in Java source style, matching the
// dex_java_name() SQL function.
func JavaName(desc string) string {
	if desc == "" {
		return ""
	}
	switch desc[0] {
	case 'V':
		return "void"
	case 'Z':
		return "boolean"
	case 'B':
		return "byte"
	case 'S':
		return "short"
	case 'C':
		return "char"
	case 'I':
		return "int"
	case 'J':
		return "long"
	case 'F':
		return "float"
	case 'D':
		return "double"
	case '[':
		return JavaName(desc[1:]) + "[]"
	case 'L':
		if len(desc) > 2 && desc[len(desc)-1] == ';' {
			s := desc[1 : len(desc)-1]
			out := make([]byte, len(s))
			for i := 0; i < len(s); i++ {
				if s[i] == '/' {
					out[i] = '.'
				} else {
					out[i] = s[i]
				}
			}
			return string(out)
		}
	}
	return desc
}

// ShortyOf builds the DEX shorty descriptor for a method signature.
func ShortyOf(returnDesc string, params []string) string {
	buf := make([]byte, 0, len(params)+1)
	add := func(d string) {
		if d == "" {
			buf = append(buf, 'V')
			return
		}
		switch d[0] {
		case 'L', '[':
			buf = append(buf, 'L')
		default:
			buf = append(buf, d[0])
		}
	}
	add(returnDesc)
	for _, p := range params {
		add(p)
	}
	return string(buf)
}

// MUTF8String decodes a MUTF-8 byte slice; exported for tests and callers that
// decode string_data payloads themselves.
func MUTF8String(b []byte) string { return decodeMUTF8(b) }
