package dextest

// OpsDex returns a one-method DEX that exercises operand ordering for the
// most error-prone Dalvik formats: 12x, 11n, 22c, 22t and 22s.
func OpsDex() []byte {
	b := New()
	ops := b.Method("Ldemo/Ops;", "ops", "V", "I")
	field := b.Field("Ldemo/Ops;", "f", "I")
	insns := []uint16{
		0x1012,    // const/4 v0, #1
		0x1032, 4, // if-eq v0, v1, :L5
		0x1059, uint16(field), // iput v0, v1, f
		0xF112,         // const/4 v1, #-1
		0x00D0, 0x0004, // add-int/lit16 v0, v0, #4
		0x000e, // return-void
	}
	b.Class(Class{
		Descriptor: "Ldemo/Ops;",
		Super:      "Ljava/lang/Object;",
		Access:     0x1,
		Direct: []Method{{
			MethodIdx: ops,
			Access:    0x9,
			Registers: 3,
			Ins:       1,
			Insns:     insns,
		}},
	})
	return b.Build()
}
