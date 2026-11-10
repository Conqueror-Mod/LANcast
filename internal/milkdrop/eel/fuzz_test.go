package eel

import "testing"

/*
 * FuzzCheck feeds the equation checker one block of a preset's code
 * (docs/hardening-plan.md, item 3). It decides what the browser will be asked
 * to compile, so it must answer for any text: never panic, and never accept a
 * block that breaks the limits it was given.
 */
func FuzzCheck(f *testing.F) {
	for _, s := range []string{
		"zoom = zoom + 0.01*sin(time);",
		"wave_r = 0.5 + 0.5*sin(time); wave_g = above(bass, 1.2)",
		"x = if(above(a,b), loop(10, y = y + 1), 0);",
		"megabuf(0) = 1; gmegabuf(1) = 2;",
		"// comment\n/* block */ q1 = 1;",
		"((((((((((((((((((((((((((x))))))))))))))))))))))))))",
		"while(1)",
		"x = ;",
		"",
	} {
		f.Add(s)
	}
	lim := DefaultLimits
	f.Fuzz(func(t *testing.T, src string) {
		if Check(src, lim) != nil {
			return
		}
		st, err := Measure(src)
		if err != nil {
			t.Fatalf("Check accepted what Measure refuses: %v", err)
		}
		if st.Bytes > lim.MaxBytes {
			t.Fatalf("accepted %d bytes over a %d limit", st.Bytes, lim.MaxBytes)
		}
		if st.Depth > lim.MaxDepth {
			t.Fatalf("accepted nesting %d over a %d limit", st.Depth, lim.MaxDepth)
		}
		if st.LoopNesting > lim.MaxLoopNesting {
			t.Fatalf("accepted %d nested loops over a %d limit", st.LoopNesting, lim.MaxLoopNesting)
		}
	})
}
