package store

import (
	"github.com/dexsql-tree/dexsql-tree/internal/dex"
)

func (l *loader) codeItems() error {
	for _, f := range l.s.Container.Files {
		for mi := range f.Methods {
			ci := f.CodeItems[uint32(mi)]
			if ci == nil {
				continue
			}
			var triesOff, handlersOff any
			if ci.TriesSize > 0 {
				toff := ci.InsnsOff + ci.InsnsSize*2
				if ci.InsnsSize%2 == 1 {
					toff += 2
				}
				triesOff = int64(toff)
				if ci.HandlersOff > 0 {
					handlersOff = int64(ci.HandlersOff)
				}
			}
			if err := l.ins("code_items", int64(f.FileID), int64(mi), int64(ci.CodeOff),
				int64(ci.RegistersSize), int64(ci.InsSize), int64(ci.OutsSize), int64(ci.TriesSize),
				int64(ci.DebugInfoOff), int64(ci.InsnsSize), int64(ci.InsnsOff), triesOff, handlersOff); err != nil {
				return err
			}
			for _, tr := range ci.Tries {
				if err := l.ins("try_items", int64(f.FileID), int64(mi), int64(tr.Index),
					int64(tr.StartAddr), int64(tr.InsnCount), int64(tr.HandlerOff)); err != nil {
					return err
				}
				for _, h := range tr.Handlers {
					var typeIdx any
					if h.HasType {
						typeIdx = int64(h.TypeIdx)
					}
					if err := l.ins("catch_handlers", int64(f.FileID), int64(mi), int64(h.HandlerIdx),
						int64(h.HandlerOff), int64(h.Ordinal), typeIdx, int64(h.Addr), boolInt(h.IsCatchAll)); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (l *loader) bytecodeRefs() error {
	for _, f := range l.s.Container.Files {
		for mi := range f.Methods {
			m := f.Methods[mi]
			ci := f.CodeItems[uint32(mi)]
			if ci == nil {
				continue
			}
			for _, ref := range ci.Refs {
				var using any
				if ref.UsingType != "" {
					using = ref.UsingType
				}
				if err := l.ins("method_refs", int64(f.FileID), int64(m.ClassIdx), int64(mi),
					ref.Kind, int64(ref.Index), using, int64(ref.Addr)); err != nil {
					return err
				}
				switch ref.Kind {
				case "string":
					val := ""
					if int(ref.Index) < len(f.Strings) {
						val = f.Strings[ref.Index]
					}
					if err := l.ins("method_strings", m.Descriptor, m.ClassDesc, m.Name, val,
						int64(f.FileID), int64(m.ClassIdx), int64(mi), int64(ref.Index)); err != nil {
						return err
					}
				case "method":
					callee, calleeClass, calleeName := "", "", ""
					if int(ref.Index) < len(f.Methods) {
						cm := f.Methods[ref.Index]
						callee, calleeClass, calleeName = cm.Descriptor, cm.ClassDesc, cm.Name
					} else {
						l.s.Warnings = append(l.s.Warnings, "method_calls: callee index out of range")
					}
					if err := l.ins("method_calls", m.Descriptor, m.ClassDesc, m.Name, callee,
						calleeClass, calleeName, int64(f.FileID), int64(m.ClassIdx), int64(mi), int64(ref.Index)); err != nil {
						return err
					}
				case "field":
					fld, fldClass, fldName, fldType := "", "", "", ""
					if int(ref.Index) < len(f.Fields) {
						fd := f.Fields[ref.Index]
						fld, fldClass, fldName, fldType = fd.Descriptor(), fd.ClassDesc, fd.Name, fd.TypeDesc
					}
					if err := l.ins("method_field_accesses", m.Descriptor, m.ClassDesc, m.Name, fld,
						fldClass, fldName, fldType, using, int64(f.FileID), int64(m.ClassIdx), int64(mi), int64(ref.Index)); err != nil {
						return err
					}
				case "type":
					desc := ""
					if int(ref.Index) < len(f.TypeDescs) {
						desc = f.TypeDescs[ref.Index]
					}
					if err := l.ins("method_used_types", m.Descriptor, m.ClassDesc, m.Name, desc,
						dex.JavaName(desc), int64(f.FileID), int64(m.ClassIdx), int64(mi), int64(ref.Index)); err != nil {
						return err
					}
				}
			}
			for _, n := range ci.Numbers {
				var dbl any
				if n.HasDouble {
					dbl = n.DoubleVal
				}
				if err := l.ins("method_numbers", int64(f.FileID), int64(m.ClassIdx), int64(mi),
					int64(n.Opcode), n.NumberType, n.LongValue, dbl); err != nil {
					return err
				}
				if err := l.ins("method_using_numbers", m.Descriptor, m.ClassDesc, m.Name, n.NumberType,
					n.LongValue, dbl, int64(n.Opcode), int64(f.FileID), int64(m.ClassIdx), int64(mi)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
