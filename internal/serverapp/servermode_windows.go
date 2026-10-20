//go:build windows

package serverapp

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"lancast/internal/desktop"
	"lancast/internal/mediatools"
	"lancast/internal/servermode"
	"lancast/internal/service"
	"lancast/internal/store"
)

/*
 * runServerMode is the elevated half of switching between a service and a
 * server that runs only while the app is open (ADR 0092).
 *
 * The app starts it with a UAC prompt and waits for it. It is never run by
 * hand in the ordinary course, and it says nothing on a console: everything
 * it has to say goes to the report file the app named, which is how a window
 * with no handle on an elevated process's output learns how far the copy has
 * got and how it ended.
 *
 * Everything only the unelevated app knows comes in as a flag — the person's
 * folder and SID above all, because an elevated process started with another
 * administrator's credentials resolves both to that other account.
 */
func runServerMode(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: lancast server-mode to-app|to-service -user-dir DIR -report FILE [-sid SID] [-addr ADDR]")
	}
	to := args[0]
	fs := flag.NewFlagSet("server-mode", flag.ContinueOnError)
	userDir := fs.String("user-dir", "", "the person's own data folder (required)")
	sid := fs.String("sid", "", "the person's account, granted their folder when the library moves into it")
	addr := fs.String("addr", ":8080", "the address the service listens on")
	reportPath := fs.String("report", "", "where progress and the outcome are written (required)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *userDir == "" || *reportPath == "" {
		return errors.New("server-mode: -user-dir and -report are required")
	}

	rep := &modeReport{path: *reportPath}
	env := servermode.Env{
		ServiceDir: service.DefaultDataDir(runtime.GOOS),
		UserDir:    filepath.Clean(*userDir),
		Addr:       *addr,
		Service:    scmService{dataDir: service.DefaultDataDir(runtime.GOOS)},
		Verify:     store.VerifyCopy,
		Answers: func(addr string, within time.Duration) bool {
			return desktop.WaitForServer(addr, within)
		},
		FreeBytes: servermode.FreeBytes,
		Report:    rep.progress,
	}
	if *sid != "" {
		s := *sid
		env.GrantUser = func(dir string) error { return servermode.GrantUser(dir, s) }
	}

	var (
		res servermode.Result
		err error
	)
	switch to {
	case "to-app":
		res, err = servermode.ToApp(env)
	case "to-service":
		res, err = servermode.ToService(env)
	default:
		err = fmt.Errorf("server-mode: unknown direction %q (want to-app or to-service)", to)
	}
	rep.finish(res, err)
	return err
}

// modeReportFile is what the app reads while a switch runs and after it ends
// (cmd/lancast, servermode_windows.go, reads the same fields).
type modeReportFile struct {
	// State is "running", "done" or "failed".
	State    string              `json:"state"`
	Progress servermode.Progress `json:"progress"`
	Result   *servermode.Result  `json:"result,omitempty"`
	Error    string              `json:"error,omitempty"`
}

type modeReport struct {
	path string
	mu   sync.Mutex
	last time.Time
	cur  modeReportFile
}

// progress is written at most a few times a second: a 10 GB copy reports
// after every megabyte, and the file is for a person watching a bar.
func (r *modeReport) progress(p servermode.Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	phaseChanged := p.Phase != r.cur.Progress.Phase
	r.cur.State, r.cur.Progress = "running", p
	if !phaseChanged && time.Since(r.last) < 250*time.Millisecond {
		return
	}
	r.last = time.Now()
	r.write()
}

func (r *modeReport) finish(res servermode.Result, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cur.Result = &res
	if err != nil {
		r.cur.State, r.cur.Error = "failed", err.Error()
	} else {
		r.cur.State = "done"
	}
	r.write()
}

// write replaces the file in one rename, so the app never reads half of it.
func (r *modeReport) write() {
	b, err := json.Marshal(r.cur)
	if err != nil {
		return
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, r.path)
}

// scmService is the installed service as a switch drives it.
type scmService struct{ dataDir string }

func (scmService) Installed() bool {
	m, err := service.NewManager()
	if err != nil {
		return false
	}
	_, err = m.Status()
	return err == nil
}

func (scmService) Stop() error { return service.StopAndWait(serviceRestartTimeout) }

func (scmService) Start() error {
	m, err := service.NewManager()
	if err != nil {
		return err
	}
	return m.Start()
}

func (scmService) SetAutomatic(on bool) error { return service.SetAutomatic(on) }

// Install registers the service exactly as `service install` does — the same
// pinned folder, the ffmpeg location recorded while it can still be seen —
// except that it starts manually until the switch has watched it answer.
func (s scmService) Install(addr string) error {
	m, err := service.NewManager()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("determine executable path: %w", err)
	}
	tools := mediatools.Detect()
	if tools != "" {
		if err := recordFFmpegDir(s.dataDir, tools); err != nil {
			return fmt.Errorf("record the ffmpeg location: %w", err)
		}
	}
	return m.Install(service.Config{DataDir: s.dataDir, Addr: addr, ExePath: exe, FFmpegDir: tools, Manual: true})
}

func (scmService) Uninstall() error {
	m, err := service.NewManager()
	if err != nil {
		return err
	}
	return m.Uninstall()
}
