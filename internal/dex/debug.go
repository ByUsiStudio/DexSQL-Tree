package dex

import "fmt"

// parseDebugFor decodes the debug_info_item attached to a code item, running
// the DEX line/position state machine to produce positions and local events.
func (f *File) parseDebugFor(ci *CodeItem, withPositions bool) {
	off := ci.DebugInfoOff
	if off == 0 || int(off) >= len(f.raw) {
		return
	}
	r := &errReader{b: f.raw, off: int(off)}
	lineStart := r.uleb128()
	params := r.uleb128()
	di := &DebugInfo{Off: off, LineStart: lineStart, ParametersSize: params}
	if params > 10000 {
		return
	}
	for i := uint32(0); i < params; i++ {
		di.ParamNameIdx = append(di.ParamNameIdx, r.uleb128())
	}
	if r.err != nil {
		return
	}
	f.DebugInfos[ci.MethodIdx] = di
	f.DebugParams[ci.MethodIdx] = di.ParamNameIdx

	// The line/local program is the expensive part; callers opt in with
	// --debug because it often dwarfs the code itself.
	if !withPositions {
		return
	}

	addr := uint32(0)
	line := int32(lineStart)
	var sourceFile uint32
	hasSourceFile := false
	var positions []Position
	var locals []Local

	for r.off < len(f.raw) {
		op := r.u1()
		if r.err != nil {
			break
		}
		switch {
		case op == 0x00: // DBG_END_SEQUENCE
			f.Positions[ci.MethodIdx] = positions
			f.Locals[ci.MethodIdx] = locals
			return
		case op == 0x01: // DBG_ADVANCE_PC
			addr += r.uleb128()
		case op == 0x02: // DBG_ADVANCE_LINE
			line += r.sleb128()
		case op == 0x03, op == 0x04: // DBG_START_LOCAL[_EXTENDED]
			reg := r.uleb128()
			nameIdx := r.uleb128()
			typeIdx := r.uleb128()
			sigIdx := uint32(0xffffffff)
			if op == 0x04 {
				sigIdx = r.uleb128()
			}
			locals = append(locals, Local{
				Address: addr, Event: "start", Register: reg,
				NameIdx: nameIdx, HasName: nameIdx != 0xffffffff,
				TypeIdx: typeIdx, HasType: typeIdx != 0xffffffff,
				SigIdx: sigIdx, HasSig: sigIdx != 0xffffffff,
			})
		case op == 0x05: // DBG_END_LOCAL
			locals = append(locals, Local{Address: addr, Event: "end", Register: r.uleb128()})
		case op == 0x06: // DBG_RESTART_LOCAL
			locals = append(locals, Local{Address: addr, Event: "restart", Register: r.uleb128()})
		case op == 0x07: // DBG_SET_PROLOGUE_END
			positions = append(positions, Position{Address: addr, Line: line, SourceFileIdx: sourceFile, HasSourceFile: hasSourceFile, PrologueEnd: true})
		case op == 0x08: // DBG_SET_EPILOGUE_BEGIN
			positions = append(positions, Position{Address: addr, Line: line, SourceFileIdx: sourceFile, HasSourceFile: hasSourceFile, EpilogueBegin: true})
		case op == 0x09: // DBG_SET_FILE
			sourceFile = r.uleb128()
			hasSourceFile = true
		default:
			adjusted := int(op) - 0x0a
			addr += uint32(adjusted / 15)
			line += int32(-4 + adjusted%15)
			positions = append(positions, Position{Address: addr, Line: line, SourceFileIdx: sourceFile, HasSourceFile: hasSourceFile})
		}
	}
	if r.err != nil {
		f.Warnings = append(f.Warnings, fmt.Sprintf("method %d debug_info: %v", ci.MethodIdx, r.err))
	}
	f.Positions[ci.MethodIdx] = positions
	f.Locals[ci.MethodIdx] = locals
}
