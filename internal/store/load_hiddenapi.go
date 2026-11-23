package store

// hiddenApi materialises the hidden API restriction table. Rows are only
// written for members that carry a non-zero flag word, so the table stays small
// (a typical release APK restricts a few thousand members, not every member).
func (l *loader) hiddenApi() error {
	for _, f := range l.s.Container.Files {
		for _, h := range f.HiddenApiFlags {
			if err := l.ins("hiddenapi_flags",
				int64(f.FileID), int64(h.ClassIdx), h.ClassDesc, h.Kind,
				int64(h.MemberIdx), int64(h.Flags), h.Restriction,
			); err != nil {
				return err
			}
		}
	}
	return nil
}
