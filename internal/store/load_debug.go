package store

func (l *loader) debugInfo() error {
	for _, f := range l.s.Container.Files {
		for mi := range f.Methods {
			di, ok := f.DebugInfos[uint32(mi)]
			if !ok {
				continue
			}
			if err := l.ins("debug_info", int64(f.FileID), int64(mi), int64(di.Off),
				int64(di.LineStart), int64(di.ParametersSize)); err != nil {
				return err
			}
			for i := 0; i < len(di.ParamNameIdx); i++ {
				var nameIdx any
				if di.ParamNameIdx[i] != 0xffffffff {
					nameIdx = int64(di.ParamNameIdx[i])
				}
				if err := l.ins("debug_parameters", int64(f.FileID), int64(mi), int64(di.Off),
					int64(i), nameIdx); err != nil {
					return err
				}
			}
			for i, p := range f.Positions[uint32(mi)] {
				var sf any
				if p.HasSourceFile {
					sf = int64(p.SourceFileIdx)
				}
				if err := l.ins("debug_positions", int64(f.FileID), int64(mi), int64(di.Off), int64(i),
					int64(p.Address), int64(p.Line), sf, boolInt(p.PrologueEnd), boolInt(p.EpilogueBegin)); err != nil {
					return err
				}
			}
			for i, loc := range f.Locals[uint32(mi)] {
				var nameIdx, typeIdx, sigIdx any
				if loc.HasName {
					nameIdx = int64(loc.NameIdx)
				}
				if loc.HasType {
					typeIdx = int64(loc.TypeIdx)
				}
				if loc.HasSig {
					sigIdx = int64(loc.SigIdx)
				}
				if err := l.ins("debug_locals", int64(f.FileID), int64(mi), int64(di.Off), int64(i),
					int64(loc.Address), loc.Event, int64(loc.Register), nameIdx, typeIdx, sigIdx); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
