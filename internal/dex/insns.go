package dex

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// opcodeNames is indexed by Dalvik opcode. "-" marks an unused slot.
const opcodeTable = `nop move move/from16 move/16 move-wide move-wide/from16 move-wide/16 move-object move-object/from16 move-object/16 move-result move-result-wide move-result-object move-exception return-void return return-wide return-object const/4 const/16 const const/high16 const-wide/16 const-wide/32 const-wide const-wide/high16 const-string const-string/jumbo const-class monitor-enter monitor-exit check-cast instance-of array-length new-instance new-array filled-new-array filled-new-array/range fill-array-data throw goto goto/16 goto/32 packed-switch sparse-switch cmpl-float cmpg-float cmpl-double cmpg-double cmp-long if-eq if-ne if-lt if-ge if-gt if-le if-eqz if-nez if-ltz if-gez if-gtz if-lez - - - - - - aget aget-wide aget-object aget-boolean aget-byte aget-char aget-short aput aput-wide aput-object aput-boolean aput-byte aput-char aput-short iget iget-wide iget-object iget-boolean iget-byte iget-char iget-short iput iput-wide iput-object iput-boolean iput-byte iput-char iput-short sget sget-wide sget-object sget-boolean sget-byte sget-char sget-short sput sput-wide sput-object sput-boolean sput-byte sput-char sput-short invoke-virtual invoke-super invoke-direct invoke-static invoke-interface - invoke-virtual/range invoke-super/range invoke-direct/range invoke-static/range invoke-interface/range - - neg-int not-int neg-long not-long neg-float neg-double int-to-long int-to-float int-to-double long-to-int long-to-float long-to-double float-to-int float-to-long float-to-double double-to-int double-to-long double-to-float int-to-byte int-to-char int-to-short add-int sub-int mul-int div-int rem-int and-int or-int xor-int shl-int shr-int ushr-int add-long sub-long mul-long div-long rem-long and-long or-long xor-long shl-long shr-long ushr-long add-float sub-float mul-float div-float rem-float add-double sub-double mul-double div-double rem-double add-int/2addr sub-int/2addr mul-int/2addr div-int/2addr rem-int/2addr and-int/2addr or-int/2addr xor-int/2addr shl-int/2addr shr-int/2addr ushr-int/2addr add-long/2addr sub-long/2addr mul-long/2addr div-long/2addr rem-long/2addr and-long/2addr or-long/2addr xor-long/2addr shl-long/2addr shr-long/2addr ushr-long/2addr add-float/2addr sub-float/2addr mul-float/2addr div-float/2addr rem-float/2addr add-double/2addr sub-double/2addr mul-double/2addr div-double/2addr rem-double/2addr add-int/lit16 rsub-int mul-int/lit16 div-int/lit16 rem-int/lit16 and-int/lit16 or-int/lit16 xor-int/lit16 add-int/lit8 rsub-int/lit8 mul-int/lit8 div-int/lit8 rem-int/lit8 and-int/lit8 or-int/lit8 xor-int/lit8 shl-int/lit8 shr-int/lit8 ushr-int/lit8 - - - - - - - - - - - - - - - - - - - - - - - invoke-polymorphic invoke-polymorphic/range invoke-custom invoke-custom/range const-method-handle const-method-type`

var opcodeNames = strings.Split(opcodeTable, " ")

func init() {
	if len(opcodeNames) != 256 {
		panic(fmt.Sprintf("dex: opcode table has %d entries, want 256", len(opcodeNames)))
	}
}

// OpcodeName returns the Dalvik mnemonic for an opcode, or "unused_XX".
func OpcodeName(op uint8) string {
	if n := opcodeNames[op]; n != "-" {
		return n
	}
	return fmt.Sprintf("unused_%02x", op)
}

// insnSize returns the instruction width in 16-bit code units for formats
// without payloads. Payload pseudo-instructions are handled by payloadSize.
func insnSize(op uint8) int {
	switch {
	case op == 0x00, op == 0x0e, op == 0x3e, op == 0x3f, op == 0x40, op == 0x41, op == 0x42, op == 0x43,
		op == 0x73, op == 0x79, op == 0x7a, op >= 0xe3 && op <= 0xf9:
		return 1
	case op == 0x02, op == 0x05, op == 0x08, op == 0x13, op == 0x15, op == 0x16, op == 0x19,
		op == 0x1a, op == 0x1c, op == 0x1f, op == 0x20, op == 0x22, op == 0x23, op == 0x29,
		op >= 0x2d && op <= 0x31, op >= 0x32 && op <= 0x3d, op >= 0x44 && op <= 0x51,
		op >= 0x52 && op <= 0x6d, op >= 0x90 && op <= 0xaf,
		op >= 0xd0 && op <= 0xe2, op == 0xfe, op == 0xff:
		return 2
	case op == 0x03, op == 0x06, op == 0x09, op == 0x14, op == 0x17, op == 0x1b, op == 0x24,
		op == 0x25, op == 0x26, op == 0x2a, op == 0x2b, op == 0x2c, op == 0x6e, op == 0x6f,
		op == 0x70, op == 0x71, op == 0x72, op == 0x74, op == 0x75, op == 0x76, op == 0x77,
		op == 0x78, op == 0xfc, op == 0xfd:
		return 3
	case op == 0xfa, op == 0xfb:
		return 4
	case op == 0x18:
		return 5
	}
	return 1
}

// codeUnitsPayload identifies payloads from the second byte of a nop unit.
func codeUnitsPayload(insns []uint16, i int) (int, string, bool) {
	u := insns[i]
	if u&0xff != 0 {
		return 0, "", false
	}
	switch u >> 8 {
	case 0x01:
		if i+1 >= len(insns) {
			return 0, "", false
		}
		size := int(insns[i+1])
		return size*2 + 4, "packed-switch-data", true
	case 0x02:
		if i+1 >= len(insns) {
			return 0, "", false
		}
		size := int(insns[i+1])
		return size*4 + 2, "sparse-switch-data", true
	case 0x03:
		if i+3 >= len(insns) {
			return 0, "", false
		}
		width := int(insns[i+1])
		size := int(uint32(insns[i+2]) | uint32(insns[i+3])<<16)
		return (size*width+1)/2 + 4, "array-data", true
	}
	return 0, "", false
}

func s8(v uint16) int8   { return int8(uint8(v)) }
func s16(v uint16) int16 { return int16(v) }
func s32(lo, hi uint16) int32 {
	return int32(uint32(lo) | uint32(hi)<<16)
}
func s64(a, b, c, d uint16) int64 {
	return int64(uint64(a) | uint64(b)<<16 | uint64(c)<<32 | uint64(d)<<48)
}

// parseCodeItem decodes a code_item and its instructions.
func (f *File) parseCodeItem(methodIdx, off uint32) (*CodeItem, error) {
	if int(off) >= len(f.raw) {
		return nil, fmt.Errorf("code_item offset %d out of range", off)
	}
	r := &errReader{b: f.raw, off: int(off)}
	ci := &CodeItem{MethodIdx: methodIdx, CodeOff: off}
	ci.RegistersSize = r.u2()
	ci.InsSize = r.u2()
	ci.OutsSize = r.u2()
	ci.TriesSize = r.u2()
	ci.DebugInfoOff = r.u4()
	ci.InsnsSize = r.u4()
	if r.err != nil {
		return nil, r.err
	}
	ci.InsnsOff = uint32(r.off)
	if !fits(ci.InsnsOff, ci.InsnsSize, 2, len(f.raw)) {
		return nil, fmt.Errorf("insns_size %d at %d exceeds the file", ci.InsnsSize, ci.InsnsOff)
	}
	ci.Insns = make([]uint16, ci.InsnsSize)
	for i := range ci.Insns {
		ci.Insns[i] = binary.LittleEndian.Uint16(f.raw[int(ci.InsnsOff)+i*2:])
	}
	r.off = int(ci.InsnsOff) + int(ci.InsnsSize)*2
	if ci.TriesSize > 0 {
		if ci.InsnsSize%2 == 1 {
			r.u2() // padding
		}
		for i := uint32(0); i < uint32(ci.TriesSize); i++ {
			ti := TryItem{
				Index:      i,
				StartAddr:  r.u4(),
				InsnCount:  uint32(r.u2()),
				HandlerOff: r.u2(),
			}
			ci.Tries = append(ci.Tries, ti)
		}
		if r.err == nil {
			handlersOff := r.off
			ci.HandlersOff = uint32(handlersOff)
			f.parseCatchHandlers(ci, handlersOff)
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	f.decodeInsns(ci)
	return ci, nil
}

// parseCatchHandlers decodes an encoded_catch_handler_list and attaches the
// expanded handlers to the try items that reference them by relative offset.
func (f *File) parseCatchHandlers(ci *CodeItem, off int) {
	r := &errReader{b: f.raw, off: off}
	count := int(r.sleb128())
	if count < 0 {
		return
	}
	handlers := map[int][]CatchHandler{}
	for i := 0; i < count; i++ {
		hOff := r.off - off
		n := int(r.sleb128())
		typed := n
		if typed < 0 {
			typed = -typed
		}
		for j := 0; j < typed; j++ {
			t := r.uleb128()
			a := r.uleb128()
			handlers[hOff] = append(handlers[hOff], CatchHandler{
				HandlerIdx: uint32(i), HandlerOff: uint16(hOff), Ordinal: uint32(j),
				TypeIdx: t, HasType: true, TypeDesc: f.typeDesc(t), Addr: a,
			})
		}
		if n <= 0 {
			a := r.uleb128()
			handlers[hOff] = append(handlers[hOff], CatchHandler{
				HandlerIdx: uint32(i), HandlerOff: uint16(hOff), Ordinal: uint32(typed), Addr: a, IsCatchAll: true,
			})
		}
		if r.err != nil {
			break
		}
	}
	for i := range ci.Tries {
		ti := &ci.Tries[i]
		ti.Handlers = append(ti.Handlers, handlers[int(ti.HandlerOff)]...)
	}
}

// decodeInsns walks the instruction stream once, recording references,
// numeric constants and a compact opcode listing.
func (f *File) decodeInsns(ci *CodeItem) {
	var refs []InsnRef
	var numbers []Number
	var ops []string
	insns := ci.Insns
	for i := 0; i < len(insns); {
		if n, _, ok := codeUnitsPayload(insns, i); ok {
			if n <= 0 || i+n > len(insns) {
				break
			}
			i += n
			continue
		}
		u := insns[i]
		op := uint8(u & 0xff)
		ops = append(ops, fmt.Sprintf("%02x", op))
		size := insnSize(op)
		if i+size > len(insns) {
			break
		}
		addr := uint32(i)
		switch op {
		case 0x1a: // const-string
			refs = append(refs, InsnRef{Kind: "string", Index: uint32(insns[i+1]), Addr: addr})
		case 0x1b: // const-string/jumbo
			refs = append(refs, InsnRef{Kind: "string", Index: uint32(insns[i+1]) | uint32(insns[i+2])<<16, Addr: addr})
		case 0x1c, 0x1f, 0x22, 0x24, 0x25, 0xfe, 0xff: // type / proto / method-handle refs
			idx := uint32(insns[i+1])
			kind := "type"
			switch op {
			case 0x24, 0x25:
				kind = "type"
			case 0xfe:
				kind = "method_handle"
			case 0xff:
				kind = "proto"
			}
			refs = append(refs, InsnRef{Kind: kind, Index: idx, Addr: addr})
		case 0x20, 0x23: // instance-of, new-array
			refs = append(refs, InsnRef{Kind: "type", Index: uint32(insns[i+1]), Addr: addr})
		case 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
			0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f,
			0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66,
			0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d:
			using := "get"
			if (op >= 0x59 && op <= 0x5f) || (op >= 0x67 && op <= 0x6d) {
				using = "put"
			}
			refs = append(refs, InsnRef{Kind: "field", Index: uint32(insns[i+1]), UsingType: using, Addr: addr})
		case 0x6e, 0x6f, 0x70, 0x71, 0x72, 0x74, 0x75, 0x76, 0x77, 0x78:
			refs = append(refs, InsnRef{Kind: "method", Index: uint32(insns[i+1]), Addr: addr})
		case 0xfa, 0xfb: // invoke-polymorphic: method + proto
			refs = append(refs, InsnRef{Kind: "method", Index: uint32(insns[i+1]), Addr: addr})
			refs = append(refs, InsnRef{Kind: "proto", Index: uint32(insns[i+3]), Addr: addr})
		case 0x12:
			numbers = append(numbers, Number{Opcode: op, NumberType: "byte", LongValue: int64(int8(uint8(u >> 12)))})
		case 0x13:
			numbers = append(numbers, Number{Opcode: op, NumberType: "short", LongValue: int64(s16(u >> 8))})
		case 0x14:
			numbers = append(numbers, Number{Opcode: op, NumberType: "int", LongValue: int64(s32(insns[i+1], insns[i+2]))})
		case 0x15:
			bits := int32(s16(u>>8)) << 16
			numbers = append(numbers, Number{Opcode: op, NumberType: "float", LongValue: int64(bits),
				DoubleVal: float64(math.Float32frombits(uint32(bits))), HasDouble: true})
		case 0x16:
			numbers = append(numbers, Number{Opcode: op, NumberType: "long", LongValue: int64(s16(u >> 8))})
		case 0x17:
			numbers = append(numbers, Number{Opcode: op, NumberType: "long", LongValue: int64(s32(insns[i+1], insns[i+2]))})
		case 0x18:
			numbers = append(numbers, Number{Opcode: op, NumberType: "long", LongValue: s64(insns[i+1], insns[i+2], insns[i+3], insns[i+4])})
		case 0x19:
			bits := int64(s16(u>>8)) << 48
			numbers = append(numbers, Number{Opcode: op, NumberType: "double", LongValue: bits,
				DoubleVal: math.Float64frombits(uint64(bits)), HasDouble: true})
		}
		i += size
	}
	ci.OpcodeCount = uint32(len(ops))
	ci.OpcodeHex = strings.Join(ops, " ")
	ci.Refs = refs
	ci.Numbers = numbers
}
