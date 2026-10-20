//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"lancast/internal/config"
	"lancast/internal/servermode"
	"lancast/internal/service"
)

/*
 * Switching between a service and a server that runs only while the app is
 * open (ADR 0092).
 *
 * This is the unelevated half, and it is the half that knows things: which
 * person's folder "only while the app is open" means, whose account to give
 * it to, which address this window is serving on, and whether the server
 * running now is one this process may stop. The elevated half
 * (internal/serverapp, `server-mode`) does the copying and the service work
 * and reports through a file, because a process started through UAC hands
 * back an exit code and nothing else.
 *
 * Bindings rather than an HTTP route, for the reasons the ADR gives: the
 * destination is this person's folder, the consent prompt can only be shown
 * here, and the server cannot move the database it has open.
 */

// modeSwitch is the one switch this window may have running.
type modeSwitch struct {
	mu      sync.Mutex
	running bool
	// stage is this process's own part: "elevating" while the helper runs,
	// "starting" while the right server comes up, "" otherwise.
	stage string
	// outcome is set when a switch ends, and kept for the page to read.
	outcome map[string]any
}

var switching modeSwitch

// modeReportPath is where the helper writes. In the window's own folder, so it
// is the same file however the helper was elevated.
func modeReportPath() string {
	return filepath.Join(clientDataDir(), "server-mode.json")
}

// modeReport is the helper's file (serverapp.modeReportFile).
type modeReport struct {
	State    string              `json:"state"`
	Progress servermode.Progress `json:"progress"`
	Result   *servermode.Result  `json:"result,omitempty"`
	Error    string              `json:"error,omitempty"`
}

func readModeReport() (modeReport, bool) {
	var r modeReport
	b, err := os.ReadFile(modeReportPath())
	if err != nil {
		return r, false
	}
	return r, json.Unmarshal(b, &r) == nil
}

/*
 * serverMode reports which mode this machine is in, and whether this window
 * can switch it.
 *
 * The mode is read from the same two facts everything else reads — a service
 * registration and a database in the machine-wide folder — so it cannot
 * disagree with what the next launch will do.
 */
func (l *launcher) serverMode() map[string]any {
	shared := service.DefaultDataDir(runtime.GOOS)
	_, installed := installedServiceState()
	inShared := servermode.HasLibrary(shared)

	mode := "app"
	if installed && inShared {
		mode = "service"
	}
	state := map[string]any{
		"mode": mode,
		// A registration with no library in its folder is the tail of an
		// interrupted switch; switching to the app finishes it.
		"leftover_service": installed && !inShared,
		"addr":             l.addr,
	}
	if reason := l.switchBlocked(mode, installed && !inShared); reason != "" {
		state["blocked"] = reason
	}
	switching.mu.Lock()
	state["switching"] = switching.running
	switching.mu.Unlock()
	return state
}

// switchBlocked is why this window cannot offer a switch, "" when it can.
func (l *launcher) switchBlocked(mode string, leftover bool) string {
	switch {
	case !l.target.local:
		return "This window is showing a server on another computer. Switch it from that computer."
	case l.dataDir != "":
		return "This window was started with its own -data folder, so it does not use either of the folders a switch moves between."
	}
	if mode == "app" && !leftover && l.started == nil && l.serverHolder() == "other" {
		return "Another LANcast server is running that this window did not start. Close it first, then switch."
	}
	return ""
}

// startServerModeSwitch begins a switch and returns at once. The window's
// bindings are called on its own thread, and a copy of a whole library
// cannot be waited for there.
func (l *launcher) startServerModeSwitch(to string) map[string]any {
	if to != "service" && to != "app" {
		return map[string]any{"ok": false, "error": "switch to service or app"}
	}
	state := l.serverMode()
	if reason, ok := state["blocked"].(string); ok {
		return map[string]any{"ok": false, "error": reason}
	}
	if state["mode"] == to && !(to == "app" && state["leftover_service"] == true) {
		return map[string]any{"ok": false, "error": "LANcast already runs this way"}
	}

	switching.mu.Lock()
	if switching.running {
		switching.mu.Unlock()
		return map[string]any{"ok": false, "error": "a switch is already running"}
	}
	switching.running, switching.stage, switching.outcome = true, "elevating", nil
	switching.mu.Unlock()
	_ = os.Remove(modeReportPath())

	go func() {
		outcome := l.runServerModeSwitch(to)
		switching.mu.Lock()
		switching.running, switching.stage, switching.outcome = false, "", outcome
		switching.mu.Unlock()
	}()
	return map[string]any{"ok": true}
}

// serverModeProgress is what the page polls while a switch runs, and reads
// once more when it has ended.
func serverModeProgress() map[string]any {
	switching.mu.Lock()
	out := map[string]any{"running": switching.running, "stage": switching.stage}
	if switching.outcome != nil {
		out["outcome"] = switching.outcome
	}
	switching.mu.Unlock()
	if r, ok := readModeReport(); ok {
		out["progress"] = r.Progress
	}
	return out
}

/*
 * runServerModeSwitch does the switch, start to finish, and returns what the
 * page should say about it.
 *
 * Whatever happens in the middle, it ends with a server answering: the new
 * one, or the old one put back. A window left looking at nothing is the one
 * outcome this must not have.
 */
func (l *launcher) runServerModeSwitch(to string) map[string]any {
	log := slog.Default().With("switch", to)
	userDir, err := config.DefaultDataDir()
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	sid, err := currentUserSID()
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}

	// The server the app started holds the database, so it stops first —
	// before the prompt, because the helper's first act is to copy.
	if to == "service" && l.started != nil {
		l.stopStartedServer()
		l.started = nil
	}

	log.Info("server mode switch starting", "user_dir", userDir, "addr", l.addr)
	helperErr := elevateAndWait("server-mode", "to-"+to,
		"-user-dir", userDir, "-sid", sid, "-addr", l.addr, "-report", modeReportPath())
	report, haveReport := readModeReport()

	outcome := map[string]any{"ok": false, "to": to}
	switch {
	case errors.Is(helperErr, errUACRefused):
		outcome["error"] = "Permission was not given, so nothing was changed."
	case haveReport && report.State == "done":
		outcome["ok"] = true
		outcome["result"] = report.Result
	case haveReport && report.Error != "":
		outcome["error"] = report.Error
	case helperErr != nil:
		outcome["error"] = helperErr.Error()
	default:
		outcome["error"] = "the switch ended without saying how"
	}
	log.Info("server mode switch ended", "ok", outcome["ok"], "error", outcome["error"])

	// Whichever server should be answering now, make sure it is: the app's
	// own after a switch away from the service or a failed switch to one, and
	// the service otherwise — which ensureServer waits for rather than races.
	switching.mu.Lock()
	switching.stage = "starting"
	switching.mu.Unlock()
	if err := l.ensureServer(); err != nil {
		outcome["server_error"] = err.Error()
	}
	return outcome
}

// errUACRefused is the person answering No to the consent prompt.
var errUACRefused = errors.New("permission was not granted")

// shellExecuteInfo is SHELLEXECUTEINFOW. Go lays it out with the same
// alignment as the C struct on both 386 and amd64.
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
)

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

/*
 * elevateAndWait runs this executable elevated and waits for it to exit.
 *
 * serverapp's elevateSelf does not wait, and does not need to: what it
 * starts is a service, and the port answering is the news. Here the helper's
 * exit is the news — its report is final only once it has gone — so this is
 * ShellExecuteEx, which hands back the process, rather than ShellExecute.
 */
func elevateAndWait(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = windows.EscapeArg(a)
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	params, err := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	if err != nil {
		return err
	}
	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: params,
		nShow:        windows.SW_HIDE,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	ok, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return errUACRefused
		}
		return fmt.Errorf("ask for permission: %w", callErr)
	}
	if info.hProcess == 0 {
		return errors.New("the switch was started but Windows gave no handle to wait on")
	}
	defer windows.CloseHandle(info.hProcess)
	if _, err := windows.WaitForSingleObject(info.hProcess, windows.INFINITE); err != nil {
		return err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("the switch exited with code %d", code)
	}
	return nil
}

// currentUserSID is the signed-in person's account, as the helper needs it.
func currentUserSID() (string, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("read your account: %w", err)
	}
	return tu.User.Sid.String(), nil
}

// serverModeBindings are the switch's functions for the page.
func (l *launcher) serverModeBindings() map[string]any {
	return map[string]any{
		"lancastServerMode":         l.serverMode,
		"lancastSwitchServerMode":   l.startServerModeSwitch,
		"lancastServerModeProgress": serverModeProgress,
	}
}
