package store

import (
	"github.com/byusi-studio/dexsql-tree/internal/dex"
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
	refs := l.batch("method_refs")
	calls := l.batch("method_calls")
	strs := l.batch("method_strings")
	fields := l.batch("method_field_accesses")
	usedTypes := l.batch("method_used_types")
	numbers := l.batch("method_numbers")
	usingNumbers := l.batch("method_using_numbers")
	if refs == nil && calls == nil && strs == nil && fields == nil &&
		usedTypes == nil && numbers == nil && usingNumbers == nil {
		return nil
	}
	for _, f := range l.s.Container.Files {
		for mi := range f.Methods {
			m := f.Methods[mi]
			ci := f.CodeItems[uint32(mi)]
			if ci == nil {
				continue
			}
			for _, ref := range ci.Refs {
				if refs != nil {
					var using any
					if ref.UsingType != "" {
						using = ref.UsingType
					}
					refs.rows = append(refs.rows, int64(f.FileID), int64(m.ClassIdx), int64(mi),
						ref.Kind, int64(ref.Index), using, int64(ref.Addr))
					if err := l.done(refs); err != nil {
						return err
					}
				}
				switch ref.Kind {
				case "string":
					if strs == nil {
						continue
					}
					val := ""
					if int(ref.Index) < len(f.Strings) {
						val = f.Strings[ref.Index]
					}
					strs.rows = append(strs.rows, m.Descriptor, m.ClassDesc, m.Name, val,
						int64(f.FileID), int64(m.ClassIdx), int64(mi), int64(ref.Index))
					if err := l.done(strs); err != nil {
						return err
					}
				case "method":
					if calls == nil {
						continue
					}
					callee, calleeClass, calleeName := "", "", ""
					if int(ref.Index) < len(f.Methods) {
						cm := f.Methods[ref.Index]
						callee, calleeClass, calleeName = cm.Descriptor, cm.ClassDesc, cm.Name
					} else {
						l.s.Warnings = append(l.s.Warnings, "method_calls: callee index out of range")
					}
					calls.rows = append(calls.rows, m.Descriptor, m.ClassDesc, m.Name, callee,
						calleeClass, calleeName, int64(f.FileID), int64(m.ClassIdx), int64(mi), int64(ref.Index))
					if err := l.done(calls); err != nil {
						return err
					}
				case "field":
					if fields == nil {
						continue
					}
					var using any
					if ref.UsingType != "" {
						using = ref.UsingType
					}
					fld, fldClass, fldName, fldType := "", "", "", ""
					if int(ref.Index) < len(f.Fields) {
						fd := f.Fields[ref.Index]
						fld, fldClass, fldName, fldType = fd.Descriptor(), fd.ClassDesc, fd.Name, fd.TypeDesc
					}
					fields.rows = append(fields.rows, m.Descriptor, m.ClassDesc, m.Name, fld,
						fldClass, fldName, fldType, using, int64(f.FileID), int64(m.ClassIdx),
						int64(mi), int64(ref.Index))
					if err := l.done(fields); err != nil {
						return err
					}
				case "type":
					if usedTypes == nil {
						continue
					}
					desc := ""
					if int(ref.Index) < len(f.TypeDescs) {
						desc = f.TypeDescs[ref.Index]
					}
					usedTypes.rows = append(usedTypes.rows, m.Descriptor, m.ClassDesc, m.Name, desc,
						dex.JavaName(desc), int64(f.FileID), int64(m.ClassIdx), int64(mi), int64(ref.Index))
					if err := l.done(usedTypes); err != nil {
						return err
					}
				}
			}
			for _, n := range ci.Numbers {
				if numbers != nil {
					var dbl any
					if n.HasDouble {
						dbl = n.DoubleVal
					}
					numbers.rows = append(numbers.rows, int64(f.FileID), int64(m.ClassIdx), int64(mi),
						int64(n.Opcode), n.NumberType, n.LongValue, dbl)
					if err := l.done(numbers); err != nil {
						return err
					}
				}
				if usingNumbers == nil {
					continue
				}
				var dbl any
				if n.HasDouble {
					dbl = n.DoubleVal
				}
				usingNumbers.rows = append(usingNumbers.rows, m.Descriptor, m.ClassDesc, m.Name, n.NumberType,
					n.LongValue, dbl, int64(n.Opcode), int64(f.FileID), int64(m.ClassIdx), int64(mi))
				if err := l.done(usingNumbers); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
