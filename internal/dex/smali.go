package dex

import (
	"fmt"
	"strconv"
	"strings"
)

// Smali renders a method's instructions as baksmali-style text. Branch targets
// are emitted as labels. The output is intentionally readable rather than
// byte-exact: it is aimed at query results and AI agents inspecting a match.
func (f *File) Smali(methodIdx uint32) (string, error) {
	ci := f.CodeItems[methodIdx]
	if ci == nil {
		return "", fmt.Errorf("method %d has no code item", methodIdx)
	}
	opcodes := ci.Insns
	unit := func(i int) uint16 {
		if i >= 0 && i < len(opcodes) {
			return opcodes[i]
		}
		return 0
	}

	// Collect branch targets so labels can be emitted inline.
	targets := map[uint32]bool{}
	for i := 0; i < len(opcodes); {
		if n, _, ok := codeUnitsPayload(opcodes, i); ok {
			if n <= 0 {
				break
			}
			i += n
			continue
		}
		u := opcodes[i]
		op := uint8(u & 0xff)
		switch op {
		case 0x28:
			targets[uint32(int32(i)+int32(int8(uint8(u>>8))))] = true
		case 0x29:
			targets[uint32(int32(i)+int32(s16(unit(i+1))))] = true
		case 0x2a:
			targets[uint32(int32(i)+s32(unit(i+1), unit(i+2)))] = true
		case 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d:
			targets[uint32(int32(i)+int32(s16(unit(i+1))))] = true
		}
		i += insnSize(op)
	}
	var out strings.Builder
	fmt.Fprintf(&out, ".method %s\n", f.Methods[methodIdx].Descriptor)
	fmt.Fprintf(&out, "    .registers %d\n\n", ci.RegistersSize)

	reg := func(n int) string { return "v" + strconv.Itoa(n) }
	regs35 := func(u0, u2 uint16) string {
		a := int(u0 >> 12)
		g := int(u0>>8) & 0xf
		c := int(u2 & 0xf)
		d := int((u2 >> 4) & 0xf)
		e := int((u2 >> 8) & 0xf)
		ff := int(u2 >> 12)
		all := []int{c, d, e, ff, g}
		parts := make([]string, 0, a)
		for i := 0; i < a && i < len(all); i++ {
			parts = append(parts, reg(all[i]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	regs3rc := func(u0, u2 uint16) string {
		a := int(u0 >> 8)
		c := int(u2)
		if a == 0 {
			return "{}"
		}
		return fmt.Sprintf("{v%d .. v%d}", c, c+a-1)
	}
	str := func(idx uint32) string {
		s := f.str(idx)
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\"", "\\\"")
		s = strings.ReplaceAll(s, "\n", "\\n")
		s = strings.ReplaceAll(s, "\r", "\\r")
		s = strings.ReplaceAll(s, "\t", "\\t")
		return "\"" + s + "\""
	}
	fieldRef := func(idx uint32) string {
		if int(idx) < len(f.Fields) {
			return f.Fields[idx].Descriptor()
		}
		return "field@" + strconv.Itoa(int(idx))
	}
	methodRef := func(idx uint32) string {
		if int(idx) < len(f.Methods) {
			return f.Methods[idx].Descriptor
		}
		return "method@" + strconv.Itoa(int(idx))
	}
	protoRef := func(idx uint32) string {
		if int(idx) < len(f.Protos) {
			p := f.Protos[idx]
			return "(" + strings.Join(p.ParamDescs, "") + ")" + p.ReturnDesc
		}
		return "proto@" + strconv.Itoa(int(idx))
	}
	typeRef := func(idx uint32) string { return f.typeDesc(idx) }

	for i := 0; i < len(opcodes); {
		if targets[uint32(i)] {
			fmt.Fprintf(&out, "    :L%x\n", i)
		}
		if n, kind, ok := codeUnitsPayload(opcodes, i); ok {
			fmt.Fprintf(&out, "    # %s payload (%d code units)\n", kind, n)
			if n <= 0 {
				break
			}
			i += n
			continue
		}
		u := opcodes[i]
		op := uint8(u & 0xff)
		size := insnSize(op)
		a := int(u >> 8)
		aa := int(u >> 8)
		line := ""
		switch {
		case op == 0x00:
			line = "nop"
		case op == 0x01 || op == 0x04 || op == 0x07:
			line = fmt.Sprintf("%s %s, %s", OpcodeName(op), reg(a&0xf), reg(a>>4))
		case op == 0x02 || op == 0x05 || op == 0x08:
			line = fmt.Sprintf("%s %s, %s", OpcodeName(op), reg(aa), reg(int(unit(i+1))))
		case op == 0x03 || op == 0x06 || op == 0x09:
			line = fmt.Sprintf("%s %s, %s", OpcodeName(op), reg(int(unit(i+1))), reg(int(unit(i+2))))
		case op == 0x0a || op == 0x0b || op == 0x0c || op == 0x0d:
			line = fmt.Sprintf("%s %s", OpcodeName(op), reg(aa))
		case op >= 0x0e && op <= 0x11:
			if op == 0x0e {
				line = "return-void"
			} else {
				line = fmt.Sprintf("%s %s", OpcodeName(op), reg(aa))
			}
		case op == 0x12:
			line = fmt.Sprintf("const/4 %s, #%d", reg(a&0xf), int8(uint8(a>>4)<<4)>>4)
		case op == 0x13:
			line = fmt.Sprintf("const/16 %s, #%d", reg(aa), s16(u))
		case op == 0x14:
			line = fmt.Sprintf("const %s, #%d", reg(aa), s32(unit(i+1), unit(i+2)))
		case op == 0x15:
			line = fmt.Sprintf("const/high16 %s, #%d", reg(aa), int32(s16(u))<<16)
		case op == 0x16:
			line = fmt.Sprintf("const-wide/16 %s, #%d", reg(aa), s16(u))
		case op == 0x17:
			line = fmt.Sprintf("const-wide/32 %s, #%d", reg(aa), s32(unit(i+1), unit(i+2)))
		case op == 0x18:
			line = fmt.Sprintf("const-wide %s, #%d", reg(aa), s64(unit(i+1), unit(i+2), unit(i+3), unit(i+4)))
		case op == 0x19:
			line = fmt.Sprintf("const-wide/high16 %s, #%d", reg(aa), int64(s16(u))<<48)
		case op == 0x1a:
			line = fmt.Sprintf("const-string %s, %s", reg(aa), str(uint32(unit(i+1))))
		case op == 0x1b:
			line = fmt.Sprintf("const-string/jumbo %s, %s", reg(aa), str(uint32(unit(i+1))|uint32(unit(i+2))<<16))
		case op == 0x1c:
			line = fmt.Sprintf("const-class %s, %s", reg(aa), typeRef(uint32(unit(i+1))))
		case op == 0x1d || op == 0x1e:
			line = fmt.Sprintf("%s %s", OpcodeName(op), reg(aa))
		case op == 0x1f:
			line = fmt.Sprintf("check-cast %s, %s", reg(aa), typeRef(uint32(unit(i+1))))
		case op == 0x20:
			line = fmt.Sprintf("instance-of %s, %s, %s", reg(a&0xf), reg(a>>4), typeRef(uint32(unit(i+1))))
		case op == 0x21:
			line = fmt.Sprintf("array-length %s, %s", reg(a&0xf), reg(a>>4))
		case op == 0x22:
			line = fmt.Sprintf("new-instance %s, %s", reg(aa), typeRef(uint32(unit(i+1))))
		case op == 0x23:
			line = fmt.Sprintf("new-array %s, %s, %s", reg(a&0xf), reg(a>>4), typeRef(uint32(unit(i+1))))
		case op == 0x24:
			line = fmt.Sprintf("filled-new-array %s, %s", regs35(u, unit(i+2)), typeRef(uint32(unit(i+1))))
		case op == 0x25:
			line = fmt.Sprintf("filled-new-array/range %s, %s", regs3rc(u, unit(i+2)), typeRef(uint32(unit(i+1))))
		case op == 0x26:
			line = fmt.Sprintf("fill-array-data %s, :L%x", reg(aa), int32(i)+s32(unit(i+1), unit(i+2)))
		case op == 0x27:
			line = fmt.Sprintf("throw %s", reg(aa))
		case op == 0x28:
			line = fmt.Sprintf("goto :L%x", i+int(int8(uint8(u>>8))))
		case op == 0x29:
			line = fmt.Sprintf("goto/16 :L%x", i+int(s16(unit(i+1))))
		case op == 0x2a:
			line = fmt.Sprintf("goto/32 :L%x", i+int(s32(unit(i+1), unit(i+2))))
		case op == 0x2b:
			line = fmt.Sprintf("packed-switch %s, :L%x", reg(aa), i+int(s32(unit(i+1), unit(i+2))))
		case op == 0x2c:
			line = fmt.Sprintf("sparse-switch %s, :L%x", reg(aa), i+int(s32(unit(i+1), unit(i+2))))
		case op >= 0x2d && op <= 0x31:
			line = fmt.Sprintf("%s %s, %s, %s", OpcodeName(op), reg(aa), reg(int(unit(i+1))), reg(int(unit(i+1)>>8)))
		case op >= 0x32 && op <= 0x37:
			line = fmt.Sprintf("%s %s, %s, :L%x", OpcodeName(op), reg(a&0xf), reg(a>>4), i+int(s16(unit(i+1))))
		case op >= 0x38 && op <= 0x3d:
			line = fmt.Sprintf("%s %s, :L%x", OpcodeName(op), reg(aa), i+int(s16(unit(i+1))))
		case op >= 0x44 && op <= 0x51:
			line = fmt.Sprintf("%s %s, %s, %s", OpcodeName(op), reg(aa), reg(int(unit(i+1))), reg(int(unit(i+1)>>8)))
		case op >= 0x52 && op <= 0x5f:
			line = fmt.Sprintf("%s %s, %s, %s", OpcodeName(op), reg(a&0xf), reg(a>>4), fieldRef(uint32(unit(i+1))))
		case op >= 0x60 && op <= 0x6d:
			line = fmt.Sprintf("%s %s, %s", OpcodeName(op), reg(aa), fieldRef(uint32(unit(i+1))))
		case op >= 0x6e && op <= 0x72:
			line = fmt.Sprintf("%s %s, %s", OpcodeName(op), regs35(u, unit(i+2)), methodRef(uint32(unit(i+1))))
		case op >= 0x74 && op <= 0x78:
			line = fmt.Sprintf("%s %s, %s", OpcodeName(op), regs3rc(u, unit(i+2)), methodRef(uint32(unit(i+1))))
		case op >= 0x7b && op <= 0x8f:
			line = fmt.Sprintf("%s %s, %s", OpcodeName(op), reg(a&0xf), reg(a>>4))
		case op >= 0x90 && op <= 0xaf:
			line = fmt.Sprintf("%s %s, %s, %s", OpcodeName(op), reg(aa), reg(int(unit(i+1))), reg(int(unit(i+1)>>8)))
		case op >= 0xb0 && op <= 0xcf:
			line = fmt.Sprintf("%s %s, %s", OpcodeName(op), reg(a&0xf), reg(a>>4))
		case op >= 0xd0 && op <= 0xd7:
			line = fmt.Sprintf("%s %s, %s, #%d", OpcodeName(op), reg(a&0xf), reg(a>>4), s16(unit(i+1)))
		case op >= 0xd8 && op <= 0xe2:
			line = fmt.Sprintf("%s %s, %s, #%d", OpcodeName(op), reg(aa), reg(int(unit(i+1))), int8(uint8(unit(i+1)>>8)))
		case op == 0xfa:
			line = fmt.Sprintf("invoke-polymorphic %s, %s, %s", regs35(u, unit(i+2)), methodRef(uint32(unit(i+1))), protoRef(uint32(unit(i+3))))
		case op == 0xfb:
			line = fmt.Sprintf("invoke-polymorphic/range %s, %s, %s", regs3rc(u, unit(i+2)), methodRef(uint32(unit(i+1))), protoRef(uint32(unit(i+3))))
		case op == 0xfc:
			line = fmt.Sprintf("invoke-custom %s, call_site@%d", regs35(u, unit(i+2)), uint32(unit(i+1)))
		case op == 0xfd:
			line = fmt.Sprintf("invoke-custom/range %s, call_site@%d", regs3rc(u, unit(i+2)), uint32(unit(i+1)))
		case op == 0xfe:
			line = fmt.Sprintf("const-method-handle %s, method_handle@%d", reg(aa), uint32(unit(i+1)))
		case op == 0xff:
			line = fmt.Sprintf("const-method-type %s, %s", reg(aa), protoRef(uint32(unit(i+1))))
		default:
			line = fmt.Sprintf("%s", OpcodeName(op))
		}
		fmt.Fprintf(&out, "    %s\n", line)
		i += size
	}
	out.WriteString(".end method\n")
	return out.String(), nil
}
