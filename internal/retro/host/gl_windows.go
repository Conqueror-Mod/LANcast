//go:build windows

package host

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"lancast/internal/retro/libretro"
)

/*
 * WGL: the OpenGL path a hardware-rendering core draws through (ADR 0073,
 * stage 3), entirely through syscall like everything else in this client.
 *
 * The core draws into a framebuffer object this sink owns. Each frame, the
 * part it drew is blitted into the video window at the picture's place —
 * integer-scaled and letterboxed by the same Layout the GDI path uses — and
 * the buffers are swapped.
 *
 * One rule governs every call here: **no float arguments.** syscall passes
 * arguments in integer registers, and the Windows x64 ABI passes floats in
 * XMM registers, so a float would arrive as garbage. That is why the bars are
 * cleared with glClearBufferfv (a pointer to floats) rather than glClearColor,
 * and why nothing here sets a depth range or a line width. The core calls
 * whatever it likes itself; it is C.
 *
 * Proven on the development machine against a test core that renders through
 * a 3.3 core-profile context (testdata/glcore.c). Not proven against a real
 * N64 core or on any other GPU: the plan budgets a round of fixes for exactly
 * that, and drivers differ.
 */

var (
	opengl32             = windows.NewLazySystemDLL("opengl32.dll")
	procWglCreateContext = opengl32.NewProc("wglCreateContext")
	procWglMakeCurrent   = opengl32.NewProc("wglMakeCurrent")
	procWglDeleteContext = opengl32.NewProc("wglDeleteContext")
	procWglGetProcAddr   = opengl32.NewProc("wglGetProcAddress")
	procGlGenTextures    = opengl32.NewProc("glGenTextures")
	procGlDeleteTextures = opengl32.NewProc("glDeleteTextures")
	procGlBindTexture    = opengl32.NewProc("glBindTexture")
	procGlTexImage2D     = opengl32.NewProc("glTexImage2D")
	procGlTexParameteri  = opengl32.NewProc("glTexParameteri")
	procGlViewport       = opengl32.NewProc("glViewport")
	procGlReadPixels     = opengl32.NewProc("glReadPixels")

	procChoosePixelFormat = gdi32.NewProc("ChoosePixelFormat")
	procSetPixelFormat    = gdi32.NewProc("SetPixelFormat")
	procGetPixelFormat    = gdi32.NewProc("GetPixelFormat")
	procSwapBuffers       = gdi32.NewProc("SwapBuffers")
)

const (
	glTexture2D             = 0x0DE1
	glTextureMinFilter      = 0x2801
	glTextureMagFilter      = 0x2800
	glNearest               = 0x2600
	glRGBA8                 = 0x8058
	glRGBA                  = 0x1908
	glUnsignedByte          = 0x1401
	glFramebuffer           = 0x8D40
	glReadFramebuffer       = 0x8CA8
	glDrawFramebuffer       = 0x8CA9
	glRenderbuffer          = 0x8D41
	glColorAttachment0      = 0x8CE0
	glDepthAttachment       = 0x8D00
	glDepthStencilAttach    = 0x821A
	glDepthComponent24      = 0x81A6
	glDepth24Stencil8       = 0x88F0
	glFramebufferComplete   = 0x8CD5
	glColorBufferBit        = 0x4000
	glColor                 = 0x1800
	wglContextMajor         = 0x2091
	wglContextMinor         = 0x2092
	wglContextFlags         = 0x2094
	wglContextProfileMask   = 0x9126
	wglContextCoreProfile   = 0x1
	wglContextCompatProfile = 0x2
	wglContextDebugBit      = 0x1
	pfdDrawToWindow         = 0x4
	pfdSupportOpenGL        = 0x20
	pfdDoubleBuffer         = 0x1
)

type pixelFormatDescriptor struct {
	size, version                                 uint16
	flags                                         uint32
	pixelType, colorBits                          byte
	redBits, redShift, greenBits, greenShift      byte
	blueBits, blueShift, alphaBits, alphaShift    byte
	accumBits, accumRed, accumGreen, accumBlue    byte
	accumAlpha, depthBits, stencilBits, auxBuffer byte
	layerType, reserved                           byte
	layerMask, visibleMask, damageMask            uint32
}

// WGL draws a hardware-rendering core into a window.
type WGL struct {
	HWND uintptr
	// CaptureCentre records the centre pixel of every presented frame, read
	// back from the framebuffer — the way a test sees what the core drew
	// without a person looking at the window.
	CaptureCentre bool
	LastCentre    [4]byte

	dc, rc        uintptr
	fbo, tex, rb  uint32
	width, height int
	f             glFuncs
}

// glFuncs are the GL 3 functions opengl32.dll does not export by name; they
// come from wglGetProcAddress once a context is current.
type glFuncs struct {
	genFramebuffers, bindFramebuffer, framebufferTexture2D  uintptr
	genRenderbuffers, bindRenderbuffer, renderbufferStorage uintptr
	framebufferRenderbuffer, checkFramebufferStatus         uintptr
	blitFramebuffer, clearBufferfv                          uintptr
	deleteFramebuffers, deleteRenderbuffers                 uintptr
}

func call(fn uintptr, args ...uintptr) uintptr {
	r, _, _ := syscallN(fn, args...)
	return r
}

// ProcAddress resolves a GL function: wglGetProcAddress for anything past
// GL 1.1, opengl32.dll's own exports for 1.1, which wglGetProcAddress
// answers with one of a handful of sentinel values instead.
func (g *WGL) ProcAddress(name string) uintptr {
	b, err := windows.BytePtrFromString(name)
	if err != nil {
		return 0
	}
	p, _, _ := procWglGetProcAddr.Call(uintptr(unsafe.Pointer(b)))
	switch p {
	case 0, 1, 2, 3, ^uintptr(0):
		if opengl32.Load() != nil {
			return 0
		}
		addr, err := windows.GetProcAddress(windows.Handle(opengl32.Handle()), name)
		if err != nil {
			return 0
		}
		return addr
	}
	return p
}

func (g *WGL) Init(req libretro.HWRender, maxW, maxH int) error {
	if g.HWND == 0 || maxW <= 0 || maxH <= 0 {
		return errors.New("no window or no size to render into")
	}
	dc, _, _ := procGetDC.Call(g.HWND)
	if dc == 0 {
		return errors.New("GetDC failed")
	}
	g.dc = dc
	/*
	 * A window's pixel format can be set once in its life. The video window
	 * outlives a game, so the second game finds it already set — and the
	 * format chosen here is the one it would choose again.
	 */
	if pf, _, _ := procGetPixelFormat.Call(dc); pf == 0 {
		pfd := pixelFormatDescriptor{version: 1,
			flags:     pfdDrawToWindow | pfdSupportOpenGL | pfdDoubleBuffer,
			colorBits: 32, alphaBits: 8, depthBits: 24, stencilBits: 8}
		pfd.size = uint16(unsafe.Sizeof(pfd))
		pf, _, _ := procChoosePixelFormat.Call(dc, uintptr(unsafe.Pointer(&pfd)))
		if pf == 0 {
			g.Close()
			return errors.New("no OpenGL pixel format for this window")
		}
		if ok, _, _ := procSetPixelFormat.Call(dc, pf, uintptr(unsafe.Pointer(&pfd))); ok == 0 {
			g.Close()
			return errors.New("SetPixelFormat failed")
		}
	}
	legacy, _, _ := procWglCreateContext.Call(dc)
	if legacy == 0 {
		g.Close()
		return errors.New("wglCreateContext failed: no OpenGL driver for this window")
	}
	if ok, _, _ := procWglMakeCurrent.Call(dc, legacy); ok == 0 {
		_, _, _ = procWglDeleteContext.Call(legacy)
		g.Close()
		return errors.New("wglMakeCurrent failed")
	}
	g.rc = legacy

	// A core profile, or a versioned compatibility one, needs the ARB
	// creation function, which exists only once a context is current.
	if req.Context == libretro.HWContextOpenGLCore || req.Major >= 3 {
		create := g.ProcAddress("wglCreateContextAttribsARB")
		if create == 0 {
			g.Close()
			return errors.New("this driver cannot create a versioned OpenGL context")
		}
		profile := uintptr(wglContextCompatProfile)
		if req.Context == libretro.HWContextOpenGLCore {
			profile = wglContextCoreProfile
		}
		flags := uintptr(0)
		if req.Debug {
			flags = wglContextDebugBit
		}
		major, minor := req.Major, req.Minor
		if major == 0 {
			major, minor = 3, 3
		}
		attribs := []int32{
			wglContextMajor, int32(major), wglContextMinor, int32(minor),
			wglContextProfileMask, int32(profile), wglContextFlags, int32(flags), 0,
		}
		rc := call(create, dc, 0, uintptr(unsafe.Pointer(&attribs[0])))
		if rc == 0 {
			g.Close()
			return fmt.Errorf("this GPU's driver has no OpenGL %d.%d context", major, minor)
		}
		_, _, _ = procWglMakeCurrent.Call(dc, rc)
		_, _, _ = procWglDeleteContext.Call(legacy)
		g.rc = rc
	}

	if err := g.loadFuncs(); err != nil {
		g.Close()
		return err
	}
	if err := g.makeFramebuffer(maxW, maxH, req.Depth, req.Stencil); err != nil {
		g.Close()
		return err
	}
	return nil
}

func (g *WGL) loadFuncs() error {
	for _, f := range []struct {
		dst  *uintptr
		name string
	}{
		{&g.f.genFramebuffers, "glGenFramebuffers"},
		{&g.f.bindFramebuffer, "glBindFramebuffer"},
		{&g.f.framebufferTexture2D, "glFramebufferTexture2D"},
		{&g.f.genRenderbuffers, "glGenRenderbuffers"},
		{&g.f.bindRenderbuffer, "glBindRenderbuffer"},
		{&g.f.renderbufferStorage, "glRenderbufferStorage"},
		{&g.f.framebufferRenderbuffer, "glFramebufferRenderbuffer"},
		{&g.f.checkFramebufferStatus, "glCheckFramebufferStatus"},
		{&g.f.blitFramebuffer, "glBlitFramebuffer"},
		{&g.f.clearBufferfv, "glClearBufferfv"},
		{&g.f.deleteFramebuffers, "glDeleteFramebuffers"},
		{&g.f.deleteRenderbuffers, "glDeleteRenderbuffers"},
	} {
		*f.dst = g.ProcAddress(f.name)
		if *f.dst == 0 {
			return fmt.Errorf("this driver has no %s (OpenGL 3.0 is needed)", f.name)
		}
	}
	return nil
}

func (g *WGL) makeFramebuffer(w, h int, depth, stencil bool) error {
	g.width, g.height = w, h
	call(g.f.genFramebuffers, 1, uintptr(unsafe.Pointer(&g.fbo)))
	call(g.f.bindFramebuffer, glFramebuffer, uintptr(g.fbo))

	_, _, _ = procGlGenTextures.Call(1, uintptr(unsafe.Pointer(&g.tex)))
	_, _, _ = procGlBindTexture.Call(glTexture2D, uintptr(g.tex))
	_, _, _ = procGlTexParameteri.Call(glTexture2D, glTextureMinFilter, glNearest)
	_, _, _ = procGlTexParameteri.Call(glTexture2D, glTextureMagFilter, glNearest)
	_, _, _ = procGlTexImage2D.Call(glTexture2D, 0, glRGBA8, uintptr(w), uintptr(h), 0, glRGBA, glUnsignedByte, 0)
	call(g.f.framebufferTexture2D, glFramebuffer, glColorAttachment0, glTexture2D, uintptr(g.tex), 0)

	if depth || stencil {
		call(g.f.genRenderbuffers, 1, uintptr(unsafe.Pointer(&g.rb)))
		call(g.f.bindRenderbuffer, glRenderbuffer, uintptr(g.rb))
		format, attach := uintptr(glDepthComponent24), uintptr(glDepthAttachment)
		if stencil {
			format, attach = glDepth24Stencil8, glDepthStencilAttach
		}
		call(g.f.renderbufferStorage, glRenderbuffer, format, uintptr(w), uintptr(h))
		call(g.f.framebufferRenderbuffer, glFramebuffer, attach, glRenderbuffer, uintptr(g.rb))
	}
	if st := call(g.f.checkFramebufferStatus, glFramebuffer); uint32(st) != glFramebufferComplete {
		return fmt.Errorf("the framebuffer is incomplete (status %#x)", uint32(st))
	}
	return nil
}

func (g *WGL) Framebuffer() uintptr { return uintptr(g.fbo) }

func (g *WGL) Present(width, height int, aspect float64, bottomLeftOrigin bool) {
	if g.rc == 0 || width <= 0 || height <= 0 {
		return
	}
	if width > g.width {
		width = g.width
	}
	if height > g.height {
		height = g.height
	}
	var rc winRect
	if r, _, _ := procGetClientRect.Call(g.HWND, uintptr(unsafe.Pointer(&rc))); r == 0 {
		return
	}
	ww, wh := int(rc.right-rc.left), int(rc.bottom-rc.top)
	dst := Layout(ww, wh, width, height, aspect)

	call(g.f.bindFramebuffer, glReadFramebuffer, uintptr(g.fbo))
	if g.CaptureCentre {
		var px [4]byte
		_, _, _ = procGlReadPixels.Call(uintptr(width/2), uintptr(height/2), 1, 1, glRGBA, glUnsignedByte, uintptr(unsafe.Pointer(&px[0])))
		g.LastCentre = px
	}
	call(g.f.bindFramebuffer, glDrawFramebuffer, 0)
	_, _, _ = procGlViewport.Call(0, 0, uintptr(ww), uintptr(wh))
	black := [4]float32{0, 0, 0, 1}
	call(g.f.clearBufferfv, glColor, 0, uintptr(unsafe.Pointer(&black[0])))
	if dst.W > 0 {
		// GL's window origin is bottom-left; Layout's is top-left.
		dx0, dy0 := dst.X, wh-dst.Y-dst.H
		sy0, sy1 := 0, height
		if !bottomLeftOrigin {
			sy0, sy1 = height, 0
		}
		call(g.f.blitFramebuffer,
			0, uintptr(sy0), uintptr(width), uintptr(sy1),
			uintptr(dx0), uintptr(dy0), uintptr(dx0+dst.W), uintptr(dy0+dst.H),
			glColorBufferBit, glNearest)
	}
	_, _, _ = procSwapBuffers.Call(g.dc)
	// The core binds its framebuffer from get_current_framebuffer each frame;
	// leaving it bound as well costs nothing and saves a core that assumes.
	call(g.f.bindFramebuffer, glFramebuffer, uintptr(g.fbo))
}

func (g *WGL) Close() {
	if g.rc != 0 {
		if g.f.deleteFramebuffers != 0 && g.fbo != 0 {
			call(g.f.deleteFramebuffers, 1, uintptr(unsafe.Pointer(&g.fbo)))
		}
		if g.f.deleteRenderbuffers != 0 && g.rb != 0 {
			call(g.f.deleteRenderbuffers, 1, uintptr(unsafe.Pointer(&g.rb)))
		}
		if g.tex != 0 {
			_, _, _ = procGlDeleteTextures.Call(1, uintptr(unsafe.Pointer(&g.tex)))
		}
		_, _, _ = procWglMakeCurrent.Call(0, 0)
		_, _, _ = procWglDeleteContext.Call(g.rc)
	}
	if g.dc != 0 {
		_, _, _ = procReleaseDC.Call(g.HWND, g.dc)
	}
	g.dc, g.rc, g.fbo, g.tex, g.rb = 0, 0, 0, 0, 0
	g.f = glFuncs{}
}
