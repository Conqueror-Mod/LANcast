//go:build windows

/*
 * mpvspike is the ADR 0067 Phase 0 spike, and nothing else. Throwaway.
 *
 * Question: can mpv draw video into a child window *beneath* a WebView2 whose
 * page background is transparent, with the page's controls drawn on top?
 *
 *	mpvspike.exe -mpv <libmpv-2.dll> -file <media path or URL> [-log <dir>]
 *
 * Pass criteria, checked by eye: picture shows through the page; the control
 * bar and the ticking clock draw over the picture; resize and F (fullscreen)
 * stay clean; clicking the page and Alt-Tabbing away and back keeps keys
 * working. The page also reports canPlayType for HEVC / AC-3 / E-AC-3, which
 * is printed to stdout.
 */
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
	"lancast/internal/webview2/edge"
	"lancast/internal/webview2/w32"
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	pRegisterClassEx = user32.NewProc("RegisterClassExW")
	pCreateWindowEx  = user32.NewProc("CreateWindowExW")
	pDefWindowProc   = user32.NewProc("DefWindowProcW")
	pShowWindow      = user32.NewProc("ShowWindow")
	pGetMessage      = user32.NewProc("GetMessageW")
	pTranslate       = user32.NewProc("TranslateMessage")
	pDispatch        = user32.NewProc("DispatchMessageW")
	pPostQuit        = user32.NewProc("PostQuitMessage")
	pGetClientRect   = user32.NewProc("GetClientRect")
	pMoveWindow      = user32.NewProc("MoveWindow")
	pSetWindowPos    = user32.NewProc("SetWindowPos")
	pGetWindowLong   = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLong   = user32.NewProc("SetWindowLongPtrW")
	pGetWindowRect   = user32.NewProc("GetWindowRect")
	pMonitorFromWnd  = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfo  = user32.NewProc("GetMonitorInfoW")
	pLoadCursor      = user32.NewProc("LoadCursorW")
	pClientToScreen  = user32.NewProc("ClientToScreen")
	pDestroyWindow   = user32.NewProc("DestroyWindow")
	pGetStockObject  = windows.NewLazySystemDLL("gdi32.dll").NewProc("GetStockObject")
)

const (
	wsOverlappedWindow = 0x00CF0000
	wsChild            = 0x40000000
	wsVisible          = 0x10000000
	wsClipChildren     = 0x02000000
	wsClipSiblings     = 0x04000000
	wmDestroy          = 0x0002
	wmSize             = 0x0005
	wmMove             = 0x0003
	swShow             = 5
	gwlStyle           = ^uintptr(15) // -16
	swpNoMove          = 0x0002
	swpNoSize          = 0x0001
	swpNoActivate      = 0x0010
	swpFrameChanged    = 0x0020
	swpNoZOrder        = 0x0004
)

var (
	parent, videoHwnd, popup uintptr
	browser                  *edge.Chromium
	mpv                      *mpvLib
	fullscreen               bool
	savedStyle               uintptr
	savedRect                w32.Rect
)

func main() {
	runtime.LockOSThread()
	dll := flag.String("mpv", "", "path to libmpv-2.dll")
	file := flag.String("file", "", "media path or URL")
	mode := flag.String("mode", "both", "both | video | page")
	logDir := flag.String("log", os.TempDir(), "where mpv.log goes")
	flag.Parse()
	if *dll == "" || *file == "" {
		flag.Usage()
		os.Exit(2)
	}

	var err error
	if mpv, err = loadMpv(*dll); err != nil {
		log.Fatal(err)
	}

	var hinst windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hinst)
	cls, _ := windows.UTF16PtrFromString("mpvspike")
	cursor, _, _ := pLoadCursor.Call(0, 32512)
	black, _, _ := pGetStockObject.Call(4) // BLACK_BRUSH
	wc := w32.WndClassExW{
		CbSize: uint32(unsafe.Sizeof(w32.WndClassExW{})), HInstance: hinst,
		LpszClassName: cls, LpfnWndProc: windows.NewCallback(wndproc),
		HCursor: windows.Handle(cursor), HbrBackground: windows.Handle(black),
	}
	_, _, _ = pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	title, _ := windows.UTF16PtrFromString("mpvspike - ADR 0067 Phase 0")
	parent, _, _ = pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)),
		wsOverlappedWindow|wsClipChildren, 100, 100, 1280, 760, 0, 0, uintptr(hinst), 0)

	// The video child is created first, so it sits beneath the WebView2
	// controller's window in sibling z-order.
	static, _ := windows.UTF16PtrFromString("STATIC")
	videoHwnd, _, _ = pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(static)), 0,
		wsChild|wsVisible|wsClipSiblings, 0, 0, 1280, 720, parent, 0, uintptr(hinst), 0)
	_, _, _ = pShowWindow.Call(parent, swShow)

	must(mpv.opt("wid", strconv.FormatUint(uint64(videoHwnd), 10)))
	for _, kv := range [][2]string{
		// ADR 0067: nothing loads that could reach out on its own.
		{"config", "no"}, {"load-scripts", "no"}, {"ytdl", "no"}, {"input-default-bindings", "no"},
		{"input-vo-keyboard", "no"}, {"osc", "no"}, {"keep-open", "yes"}, {"hwdec", "auto-safe"}, {"d3d11-flip", flipModel},
		{"log-file", filepath.Join(*logDir, "mpvspike-mpv.log")},
	} {
		must(mpv.opt(kv[0], kv[1]))
	}
	must(mpv.init())
	if *mode == "video" {
		must(mpv.command("loadfile", *file))
		pump()
		return
	}

	_ = os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", "0")
	browser = edge.NewChromium()
	browser.DataPath = filepath.Join(os.TempDir(), "mpvspike-webview2")
	browser.MessageCallback = onMessage
	host := parent
	if *mode == "popup" {
		// WS_POPUP, owned by the video window so it minimises and z-orders with
		// it; WS_EX_NOREDIRECTIONBITMAP so DWM composites the transparent page
		// straight over whatever is beneath, GPU swapchains included.
		popup, _, _ = pCreateWindowEx.Call(0x00200000|0x08000000, uintptr(unsafe.Pointer(cls)), 0,
			0x80000000|wsVisible, 0, 0, 100, 100, parent, 0, uintptr(hinst), 0)
		host = popup
		_, _, _ = pShowWindow.Call(videoHwnd, 0)
		must(mpv.opt("wid", strconv.FormatUint(uint64(parent), 10)))
	}
	if !browser.Embed(host) {
		log.Fatal("webview2 embed failed")
	}
	if err := browser.SetTransparentBackground(); err != nil {
		log.Fatalf("transparent background: %v", err)
	}
	// WebView2 inserts its host window beneath existing siblings, so the
	// video child has to be sent to the bottom explicitly.
	_, _, _ = pSetWindowPos.Call(videoHwnd, 1, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	layout()
	browser.NavigateToString(page)
	browser.Focus()

	if *mode == "reparent" {
		fileArg = *file
		hinstArg = uintptr(hinst)
		clsArg = cls
		browser.Eval("setTimeout(()=>s('enter'),4000);setTimeout(()=>s('leave'),16000)")
	} else if *mode != "page" {
		must(mpv.command("loadfile", *file))
	}
	pump()
}

func pump() {
	var msg w32.Msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		_, _, _ = pTranslate.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = pDispatch.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func wndproc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case wmSize:
		layout()
		return 0
	case wmMove:
		if hwnd == parent {
			layout()
		}
		if browser != nil {
			browser.NotifyParentWindowPositionChanged()
		}
	case wmDestroy:
		if hwnd != parent {
			return 0
		}
		_, _, _ = pPostQuit.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, msg, wp, lp)
	return r
}

func layout() {
	if parent == 0 {
		return
	}
	var rc w32.Rect
	_, _, _ = pGetClientRect.Call(parent, uintptr(unsafe.Pointer(&rc)))
	if videoHwnd != 0 {
		_, _, _ = pMoveWindow.Call(videoHwnd, 0, 0, uintptr(rc.Right), uintptr(rc.Bottom), 1)
	}
	if popup != 0 {
		pt := [2]int32{0, 0}
		_, _, _ = pClientToScreen.Call(parent, uintptr(unsafe.Pointer(&pt)))
		_, _, _ = pSetWindowPos.Call(popup, 0, uintptr(pt[0]), uintptr(pt[1]), uintptr(rc.Right), uintptr(rc.Bottom), swpNoZOrder|swpNoActivate)
	}
	if browser != nil {
		browser.Resize()
	}
}

func onMessage(m string) {
	fmt.Println("page:", m)
	switch m {
	case "pause":
		_ = mpv.command("cycle", "pause")
	case "back":
		_ = mpv.command("seek", "-10")
	case "fwd":
		_ = mpv.command("seek", "30")
	case "enter":
		enterPlayback()
	case "leave":
		leavePlayback()
	case "fs":
		toggleFullscreen()
	}
}

type monitorInfo struct {
	Size    uint32
	Monitor w32.Rect
	Work    w32.Rect
	Flags   uint32
}

func toggleFullscreen() {
	if !fullscreen {
		savedStyle, _, _ = pGetWindowLong.Call(parent, gwlStyle)
		_, _, _ = pGetWindowRect.Call(parent, uintptr(unsafe.Pointer(&savedRect)))
		mon, _, _ := pMonitorFromWnd.Call(parent, 2)
		mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
		_, _, _ = pGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi)))
		_, _, _ = pSetWindowLong.Call(parent, gwlStyle, savedStyle&^wsOverlappedWindow)
		_, _, _ = pSetWindowPos.Call(parent, 0, uintptr(mi.Monitor.Left), uintptr(mi.Monitor.Top),
			uintptr(mi.Monitor.Right-mi.Monitor.Left), uintptr(mi.Monitor.Bottom-mi.Monitor.Top),
			swpNoZOrder|swpFrameChanged)
	} else {
		_, _, _ = pSetWindowLong.Call(parent, gwlStyle, savedStyle)
		_, _, _ = pSetWindowPos.Call(parent, 0, uintptr(savedRect.Left), uintptr(savedRect.Top),
			uintptr(savedRect.Right-savedRect.Left), uintptr(savedRect.Bottom-savedRect.Top),
			swpNoZOrder|swpFrameChanged)
	}
	fullscreen = !fullscreen
	browser.Focus()
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// mpvLib is the five libmpv calls the spike needs, by syscall; no cgo.
type mpvLib struct {
	h                                         uintptr
	create, setOpt, initialize, cmd, destroyP *windows.LazyProc
	errString                                 *windows.LazyProc
}

func loadMpv(path string) (*mpvLib, error) {
	d := windows.NewLazyDLL(path)
	if err := d.Load(); err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	m := &mpvLib{
		create: d.NewProc("mpv_create"), setOpt: d.NewProc("mpv_set_option_string"),
		initialize: d.NewProc("mpv_initialize"), cmd: d.NewProc("mpv_command"),
		destroyP: d.NewProc("mpv_terminate_destroy"), errString: d.NewProc("mpv_error_string"),
	}
	m.h, _, _ = m.create.Call()
	if m.h == 0 {
		return nil, fmt.Errorf("mpv_create returned null")
	}
	return m, nil
}

func (m *mpvLib) check(what string, rc uintptr) error {
	if int32(rc) >= 0 {
		return nil
	}
	s, _, _ := m.errString.Call(rc)
	return fmt.Errorf("%s: %s", what, windows.BytePtrToString((*byte)(unsafe.Pointer(s))))
}

func cstr(s string) *byte { b, _ := windows.BytePtrFromString(s); return b }

func (m *mpvLib) opt(k, v string) error {
	rc, _, _ := m.setOpt.Call(m.h, uintptr(unsafe.Pointer(cstr(k))), uintptr(unsafe.Pointer(cstr(v))))
	return m.check("option "+k, rc)
}

func (m *mpvLib) init() error {
	rc, _, _ := m.initialize.Call(m.h)
	return m.check("mpv_initialize", rc)
}

func (m *mpvLib) command(args ...string) error {
	ptrs := make([]*byte, len(args)+1)
	for i, a := range args {
		ptrs[i] = cstr(a)
	}
	rc, _, _ := m.cmd.Call(m.h, uintptr(unsafe.Pointer(&ptrs[0])))
	runtime.KeepAlive(ptrs)
	return m.check("command "+args[0], rc)
}

func (m *mpvLib) destroy() { _, _, _ = m.destroyP.Call(m.h) }

const page = `<!doctype html><html><head><style>
html,body{margin:0;height:100%;background:transparent;font:14px system-ui;color:#fff;overflow:hidden}
#bar{position:absolute;left:24px;right:24px;bottom:24px;display:flex;gap:12px;align-items:center;
 padding:12px 16px;border-radius:12px;background:rgba(10,10,14,.72);backdrop-filter:blur(8px)}
button{font:inherit;color:#fff;background:#2a2a33;border:1px solid #444;border-radius:8px;padding:6px 12px}
#badge{position:absolute;top:24px;left:24px;padding:8px 12px;border-radius:8px;background:rgba(160,20,20,.8)}
</style></head><body>
<div id="badge">HTML OVERLAY - <span id="clock"></span></div>
<div id="bar"><button onclick="s('back')">-10s</button><button onclick="s('pause')">Play/Pause</button>
<button onclick="s('fwd')">+30s</button><button onclick="s('fs')">Fullscreen (F)</button>
<span id="keys">keys: space, F, arrows</span></div>
<script>
function s(m){window.chrome.webview.postMessage(m)}
setInterval(()=>{clock.textContent=new Date().toLocaleTimeString()},250);
addEventListener('keydown',e=>{keys.textContent='last key: '+e.key;
 if(e.key===' ')s('pause');if(e.key==='f'||e.key==='F')s('fs');
 if(e.key==='ArrowLeft')s('back');if(e.key==='ArrowRight')s('fwd')});
const v=document.createElement('video');
for(const t of ['video/mp4; codecs="hvc1"','video/mp4; codecs="hev1.1.6.L93.B0"','audio/mp4; codecs="ac-3"',
 'audio/mp4; codecs="ec-3"','video/webm; codecs="av01.0.05M.08"','video/x-matroska; codecs="avc1"'])
 s('canPlayType '+t+' => "'+v.canPlayType(t)+'"');
</script></body></html>`

var flipModel = "yes"

var (
	fileArg  string
	hinstArg uintptr
	clsArg   *uint16
)

// enterPlayback moves the page into an overlay popup and starts mpv in the
// main window: the ADR 0067 shape, entered only for the length of a video.
func enterPlayback() {
	popup, _, _ = pCreateWindowEx.Call(0x00200000|0x08000000, uintptr(unsafe.Pointer(clsArg)), 0,
		0x80000000|wsVisible, 0, 0, 100, 100, parent, 0, hinstArg, 0)
	_, _, _ = pShowWindow.Call(videoHwnd, 0)
	browser.Reparent(popup)
	layout()
	_ = mpv.opt("wid", strconv.FormatUint(uint64(parent), 10))
	_ = mpv.command("loadfile", fileArg)
	browser.Focus()
}

func leavePlayback() {
	_ = mpv.command("stop")
	browser.Reparent(parent)
	_, _, _ = pDestroyWindow.Call(popup)
	popup = 0
	layout()
	browser.Focus()
}
