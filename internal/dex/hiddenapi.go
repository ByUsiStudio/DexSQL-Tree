package dex

import "fmt"

// HiddenApiFlag is the restriction recorded for one field or method inside a
// hiddenapi_class_data_item (map_list type 0xF000). Android 10+ packs this
// table into release APKs, so it is the authoritative answer to "is this
// member callable from app code".
type HiddenApiFlag struct {
	ClassIdx    uint32
	ClassDesc   string
	Kind        string // "field" | "method"
	MemberIdx   uint32
	Flags       uint32
	Restriction string // sdk | unsupported | blocked | core-platform | test
}

// Hidden API flag bits, from art/libdexfile/dex/hidden_api_access_flags.h.
const (
	hiddenSDK          = 1 << 0
	hiddenUnsupported  = 1 << 1
	hiddenBlocked      = 1 << 2
	hiddenMaxTargetO   = 1 << 3
	hiddenMaxTargetP   = 1 << 4
	hiddenMaxTargetQ   = 1 << 5
	hiddenCorePlatform = 1 << 6
	hiddenTest         = 1 << 7
)

// TypeHiddenApiClassData is the map_list entry type for hidden API flags.
const TypeHiddenApiClassData = 0xF000

// restrictionLabel turns a raw flag word into the label Android tooling shows.
// The most restrictive bit wins, which is what callers care about.
func restrictionLabel(flags uint32) string {
	switch {
	case flags&hiddenBlocked != 0:
		return "blocked"
	case flags&hiddenMaxTargetQ != 0, flags&hiddenMaxTargetP != 0, flags&hiddenMaxTargetO != 0:
		return "unsupported"
	case flags&hiddenUnsupported != 0:
		return "unsupported"
	case flags&hiddenCorePlatform != 0:
		return "core-platform"
	case flags&hiddenTest != 0:
		return "test"
	case flags&hiddenSDK != 0:
		return "sdk"
	default:
		return ""
	}
}

// parseHiddenApi reads the hidden API table, if the image has one. The table is
// a sequence of per-class runs, in class_defs order, holding one uleb128 flag
// word per member: static fields, instance fields, direct methods, then virtual
// methods. A run with no members is a single zero byte.
//
// Decoding is bounded by the declared size and by the member counts we already
// parsed, so a malformed table produces a warning instead of garbage rows.
func (f *File) parseHiddenApi(data []byte) {
	var off uint32
	found := false
	for _, m := range f.MapItems {
		if m.TypeCode == TypeHiddenApiClassData {
			off = m.Offset
			found = true
			break
		}
	}
	if !found || off == 0 {
		return
	}
	base := int(off)
	if base+4 > len(data) {
		f.Warnings = append(f.Warnings, "hiddenapi: header out of range")
		return
	}
	size := int(leUint32(data[base:]))
	if size < 4 || base+size > len(data) {
		f.Warnings = append(f.Warnings, fmt.Sprintf("hiddenapi: bad section size %d", size))
		return
	}
	end := base + size
	pos := base + 4

	out := make([]HiddenApiFlag, 0, len(f.Classes)*4)
	for ci := range f.Classes {
		c := &f.Classes[ci]
		cd := c.ClassData
		if cd == nil {
			continue
		}
		members := make([]struct {
			kind string
			idx  uint32
		}, 0, len(cd.StaticFields)+len(cd.InstanceFields)+len(cd.DirectMethods)+len(cd.VirtualMethods))
		for _, fl := range cd.StaticFields {
			members = append(members, struct {
				kind string
				idx  uint32
			}{"field", fl.FieldIdx})
		}
		for _, fl := range cd.InstanceFields {
			members = append(members, struct {
				kind string
				idx  uint32
			}{"field", fl.FieldIdx})
		}
		for _, m := range cd.DirectMethods {
			members = append(members, struct {
				kind string
				idx  uint32
			}{"method", m.MethodIdx})
		}
		for _, m := range cd.VirtualMethods {
			members = append(members, struct {
				kind string
				idx  uint32
			}{"method", m.MethodIdx})
		}
		// Each class run ends with a 0 terminator. Members with no restriction
		// are encoded as 0 too, so we read exactly len(members) values and then
		// accept a single optional terminator.
		for i := 0; i < len(members); i++ {
			if pos >= end {
				f.Warnings = append(f.Warnings, fmt.Sprintf("hiddenapi: truncated at class %d", ci))
				f.HiddenApiFlags = out
				return
			}
			v, n := uleb128At(data[pos:end])
			if n == 0 {
				f.Warnings = append(f.Warnings, fmt.Sprintf("hiddenapi: bad uleb128 at class %d", ci))
				f.HiddenApiFlags = out
				return
			}
			pos += n
			if v == 0 {
				continue
			}
			out = append(out, HiddenApiFlag{
				ClassIdx:    uint32(ci),
				ClassDesc:   c.Descriptor,
				Kind:        members[i].kind,
				MemberIdx:   members[i].idx,
				Flags:       v,
				Restriction: restrictionLabel(v),
			})
		}
		if pos < end && data[pos] == 0 {
			pos++
		}
	}
	f.HiddenApiFlags = out
}

// uleb128At decodes one unsigned LEB128 value, returning the value and the
// number of bytes consumed (0 on truncation).
func uleb128At(b []byte) (uint32, int) {
	var v uint32
	for i := 0; i < len(b) && i < 5; i++ {
		c := b[i]
		v |= uint32(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return v, i + 1
		}
	}
	return 0, 0
}

func leUint32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
