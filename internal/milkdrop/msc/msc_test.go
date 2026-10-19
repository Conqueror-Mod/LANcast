package msc

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

/*
 * One named test per decision the converter makes, in the style of
 * internal/probe/decide_test.go: what it returns, and why.
 *
 * Most run the real module, which costs about half a second to compile once
 * per converter and ~0.1 s a shader. The deadline, trap and memory cases use
 * modules of a dozen bytes assembled below instead: the real converter's hang
 * and traps were found by mutating corpus shaders, which LANcast does not
 * redistribute, and what is under test there is the sandbox, not the shader.
 */

// shared is one converter for the tests that only convert, so the module is
// compiled once for the package rather than once a test.
var (
	sharedOnce sync.Once
	shared     *Converter
)

func realConv(t *testing.T) *Converter {
	t.Helper()
	sharedOnce.Do(func() { shared = New() })
	return shared
}

// A shader as internal/milkdrop prepares one, cut down to what the converter
// needs: a sampler, a uniform and the renamed entry point.
const simple = `uniform sampler2D sampler_main;
float time;
float4 xlat_main (float2 uv : TEXCOORD0) : COLOR0
{
    float3 ret;
    ret = tex2D(sampler_main, uv).xyz * (0.5 + 0.5*sin(time));
    return float4(ret, 1.0);
}`

func TestTheModuleIsCompiledOnFirstUseNotAtNew(t *testing.T) {
	c := New()
	defer c.Close(context.Background())
	if c.Compiled() {
		t.Fatal("New compiled the module; a server that never converts a shader should not pay for it")
	}
	if _, err := c.Convert(context.Background(), simple); err != nil {
		t.Fatal(err)
	}
	if !c.Compiled() {
		t.Fatal("the module is not compiled after a conversion")
	}
}

func TestAPreparedShaderConvertsToGLSLES300(t *testing.T) {
	out, err := realConv(t).Convert(context.Background(), simple)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#version 300 es", "void main ()", "texture (sampler_main, xlv_TEXCOORD0)", "_glesFragData[0] = "} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestAnUndeclaredNameIsRefusedWithTheConvertersFirstLine(t *testing.T) {
	bad := strings.Replace(simple, "sin(time)", "sin(nosuchname)", 1)
	_, err := realConv(t).Convert(context.Background(), bad)
	var f *Failure
	if !errors.As(err, &f) {
		t.Fatalf("err = %v, want a *Failure", err)
	}
	if !strings.HasPrefix(f.Reason, "HLSL parse error:") || !strings.Contains(f.Reason, "'nosuchname' : undeclared identifier") {
		t.Fatalf("reason %q, want hlsl2glslfork's first line", f.Reason)
	}
	if strings.Contains(f.Reason, "\n") {
		t.Fatalf("reason %q has more than one line", f.Reason)
	}
}

func TestWhatTheTranslatorPassesTheOptimizerMayRefuse(t *testing.T) {
	// hlsl2glslfork translates transpose() of a non-square matrix into a call
	// to a helper it never defines, and glsl-optimizer, compiling the result,
	// refuses it. Of the corpus's 9,141 shaders, 143 fail in this second
	// stage rather than the first.
	bad := strings.Replace(simple, "ret = tex2D", "float3 v = transpose(float3x2(1,2,3,4,5,6))[0]; ret = v * tex2D", 1)
	_, err := realConv(t).Convert(context.Background(), bad)
	var f *Failure
	if !errors.As(err, &f) {
		t.Fatalf("err = %v, want a *Failure", err)
	}
	if !strings.HasPrefix(f.Reason, "glsl-optimizer:") {
		t.Fatalf("reason %q, want glsl-optimizer's", f.Reason)
	}
}

// mojoshader's preprocessor has a file reader for #include; it is never
// given one, so an #include is a refusal, not a read.
func TestAnIncludeIsRefusedNotRead(t *testing.T) {
	_, err := realConv(t).Convert(context.Background(), "#include \"msc.wasm.gz\"\n"+simple)
	var f *Failure
	if !errors.As(err, &f) || !strings.Contains(f.Reason, "no include callbacks defined") {
		t.Fatalf("err = %v, want the preprocessor's refusal", err)
	}
}

func TestAnEmptyShaderIsRefused(t *testing.T) {
	_, err := realConv(t).Convert(context.Background(), "")
	var f *Failure
	if !errors.As(err, &f) || f.Reason != "the shader is empty" {
		t.Fatalf("err = %v", err)
	}
}

func TestOutputPastTheCapIsRefused(t *testing.T) {
	c := New()
	defer c.Close(context.Background())
	c.maxOut = 64
	_, err := c.Convert(context.Background(), simple)
	var f *Failure
	if !errors.As(err, &f) || !strings.Contains(f.Reason, "is over") {
		t.Fatalf("err = %v, want the output cap's refusal", err)
	}
}

func TestOneConversionAtATime(t *testing.T) {
	c := realConv(t)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Convert(context.Background(), simple); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.maxInflight != 1 {
		t.Fatalf("%d conversions ran at once, want 1", c.maxInflight)
	}
}

// ---- the sandbox, with modules made for it ------------------------------------------

// module assembles a WebAssembly module with one memory of one page and a
// `_start` whose body is the given instructions.
func module(body ...byte) func() ([]byte, error) {
	code := append([]byte{0x00}, body...) // no locals
	code = append(code, 0x0b)             // end
	b := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // type: () -> ()
		0x03, 0x02, 0x01, 0x00, // func 0 has type 0
		0x05, 0x03, 0x01, 0x00, 0x01, // memory: min 1 page, no max
		0x07, 0x0a, 0x01, 0x06, '_', 's', 't', 'a', 'r', 't', 0x00, 0x00, // export _start
		0x0a, byte(len(code) + 2), 0x01, byte(len(code))}
	b = append(b, code...)
	return func() ([]byte, error) { return b, nil }
}

var (
	loopForever = module(0x03, 0x40, 0x0c, 0x00, 0x0b) // loop br 0 end
	trap        = module(0x00)                         // unreachable
)

// grow asks for n more pages and traps if memory.grow refuses them.
func grow(n int) func() ([]byte, error) {
	leb := []byte{}
	for v := n; ; {
		b := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
			leb = append(leb, b)
			break
		}
		leb = append(leb, b|0x80)
	}
	body := append([]byte{0x41}, leb...)
	body = append(body, 0x40, 0x00, // memory.grow 0
		0x41, 0x7f, 0x46, // i32.const -1; i32.eq
		0x04, 0x40, 0x00, 0x0b) // if unreachable end
	return module(body...)
}

func TestAHangIsStoppedAtTheDeadline(t *testing.T) {
	c := newConverter(loopForever, 200*time.Millisecond, MemoryLimitBytes)
	defer c.Close(context.Background())
	t0 := time.Now()
	_, err := c.Convert(context.Background(), "x")
	var f *Failure
	if !errors.As(err, &f) || !strings.Contains(f.Reason, "did not finish within 200ms") {
		t.Fatalf("err = %v, want the deadline's refusal", err)
	}
	if d := time.Since(t0); d > 2*time.Second {
		t.Fatalf("the hang was stopped after %v", d)
	}
}

func TestACancelledRequestIsNotTheShadersFault(t *testing.T) {
	c := newConverter(loopForever, 10*time.Second, MemoryLimitBytes)
	defer c.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := c.Convert(ctx, "x")
	var f *Failure
	if errors.As(err, &f) {
		t.Fatalf("a request that ended was recorded as the shader failing: %q", f.Reason)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the request's own", err)
	}
}

func TestATrapIsContainedAndTheNextConversionRuns(t *testing.T) {
	c := newConverter(trap, Deadline, MemoryLimitBytes)
	defer c.Close(context.Background())
	for i := 0; i < 2; i++ {
		_, err := c.Convert(context.Background(), "x")
		var f *Failure
		if !errors.As(err, &f) || !strings.HasPrefix(f.Reason, "the converter crashed: ") || !strings.Contains(f.Reason, "unreachable") {
			t.Fatalf("conversion %d: err = %v, want a contained trap", i+1, err)
		}
		if strings.Contains(f.Reason, "\n") {
			t.Fatalf("reason %q carries wazero's stack trace", f.Reason)
		}
	}
}

func TestMemoryPastTheCeilingIsRefused(t *testing.T) {
	// 32 MiB is 512 pages, and the module starts with one.
	under := newConverter(grow(500), Deadline, MemoryLimitBytes)
	defer under.Close(context.Background())
	if _, err := under.Convert(context.Background(), "x"); err != nil {
		t.Fatalf("growing to 501 pages: %v (the control: under the ceiling must work)", err)
	}
	over := newConverter(grow(512), Deadline, MemoryLimitBytes)
	defer over.Close(context.Background())
	_, err := over.Convert(context.Background(), "x")
	var f *Failure
	if !errors.As(err, &f) {
		t.Fatalf("growing to 513 pages: err = %v, want the ceiling to refuse it", err)
	}
}
