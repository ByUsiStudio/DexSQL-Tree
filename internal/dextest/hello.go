package dextest

// HelloDex returns a small DEX image containing one class:
//
//	package demo;
//	public class Hello {
//	    public static void main(String[] args) {
//	        System.out.println("Hello from DexSQL-Tree");
//	    }
//	}
//
// It exercises string constants, field access, method invocation and a call
// reference, so it is used by the parser, SQL and tree tests.
func HelloDex() []byte {
	b := New()
	msg := b.Str("Hello from DexSQL-Tree")
	main := b.Method("Ldemo/Hello;", "main", "V", "[Ljava/lang/String;")
	println := b.Method("Ljava/io/PrintStream;", "println", "V", "Ljava/lang/String;")
	outField := b.Field("Ljava/lang/System;", "out", "Ljava/io/PrintStream;")

	insns := []uint16{
		0x001a, uint16(msg), // const-string v0, msg
		0x0162, uint16(outField), // sget-object v1, System.out
		0x206e, uint16(println), 0x0001, // invoke-virtual {v1, v0}, println
		0x000e, // return-void
	}
	b.Class(Class{
		Descriptor: "Ldemo/Hello;",
		Super:      "Ljava/lang/Object;",
		SourceFile: "Hello.java",
		Access:     0x1, // public
		Direct: []Method{{
			MethodIdx: main,
			Access:    0x9, // public static
			Registers: 2,
			Ins:       1,
			Outs:      2,
			Insns:     insns,
			Debug: &Debug{
				LineStart: 3,
				Params:    []uint32{0xffffffff},
				Bytes: []byte{
					0x07, // DBG_SET_PROLOGUE_END
					0x0e, // position, addr 0, line 3
					0x1d, // addr +1, line 3
					0x26, // addr +2, line 3
					0x00, // DBG_END_SEQUENCE
				},
			},
		}},
	})
	return b.Build()
}
