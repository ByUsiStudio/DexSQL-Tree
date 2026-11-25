package store

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
	calls := l.batch("_call_sites")
	strs := l.batch("_string_refs")
	fields := l.batch("_field_accesses")
	usedTypes := l.batch("_used_types")
	numbers := l.batch("method_numbers")
	if refs == nil && calls == nil && strs == nil && fields == nil &&
		usedTypes == nil && numbers == nil {
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
					strs.rows = append(strs.rows, int64(f.FileID), int64(m.ClassIdx), int64(mi), int64(ref.Index))
					if err := l.done(strs); err != nil {
						return err
					}
				case "method":
					if calls == nil {
						continue
					}
					// Only the callee descriptor is worth storing: its class and
					// name come from the methods row the view already joins.
					callee := ""
					if int(ref.Index) < len(f.Methods) {
						callee = f.Methods[ref.Index].Descriptor
					} else {
						l.s.Warnings = append(l.s.Warnings, "method_calls: callee index out of range")
					}
					calls.rows = append(calls.rows, int64(f.FileID), int64(m.ClassIdx), int64(mi),
						int64(ref.Index), callee)
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
					fields.rows = append(fields.rows, int64(f.FileID), int64(m.ClassIdx),
						int64(mi), int64(ref.Index), using)
					if err := l.done(fields); err != nil {
						return err
					}
				case "type":
					if usedTypes == nil {
						continue
					}
					usedTypes.rows = append(usedTypes.rows, int64(f.FileID), int64(m.ClassIdx),
						int64(mi), int64(ref.Index))
					if err := l.done(usedTypes); err != nil {
						return err
					}
				}
			}
			for _, n := range ci.Numbers {
				if numbers == nil {
					continue
				}
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
		}
	}
	return nil
}
