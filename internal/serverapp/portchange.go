package serverapp

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"lancast/internal/api"
	"lancast/internal/childproc"
	"lancast/internal/desktop"
	"lancast/internal/service"
)

/*
 * Changing the port the service listens on (ADR 0089).
 *
 * The server rewrites its own service command line -- it runs as LocalSystem,
 * which may -- and hands the restart to the same detached helper an update
 * uses, told to check the new port answers. If it does not within
 * portAnswerWithin, the helper puts the old port back and starts the service
 * again. Either way it writes what happened to the data folder, which is how
 * the Network pane can say "that did not work" after a restart that has
 * replaced the process that was asked.
 */

// portAnswerWithin is how long a server gets to answer on its new port. A
// normal start on Aither takes a few seconds; Windows' own recovery restarts
// after a failed bind are slower than this, so a server that cannot listen is
// rolled back rather than left retrying.
const portAnswerWithin = 30 * time.Second

// servicePortChanger changes the installed service's port from inside it.
type servicePortChanger struct {
	dataDir string
}

func (p servicePortChanger) Change(port string) error {
	from, ok := service.ConfiguredAddr()
	if !ok {
		return errors.New("the service's configuration could not be read")
	}
	host, _, err := net.SplitHostPort(from)
	if err != nil {
		return fmt.Errorf("the service's address %q is not host:port", from)
	}
	to := net.JoinHostPort(host, port)
	if err := service.SetAddr(to); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		_ = service.SetAddr(from)
		return fmt.Errorf("locate executable: %w", err)
	}
	cmd := exec.Command(exe, "service", "restart",
		"-data", p.dataDir, "-verify", to, "-fallback", from)
	cmd.Dir = filepath.Dir(exe)
	childproc.Hide(cmd)
	childproc.Detach(cmd)
	if err := cmd.Start(); err != nil {
		// Nothing has restarted, so nothing is listening anywhere new: put
		// the command line back the way the running server matches.
		_ = service.SetAddr(from)
		return fmt.Errorf("start the restart helper: %w", err)
	}
	return nil
}

// The service calls restartAndVerify makes, replaced in tests: the rollback
// is the part that matters and the one a real service manager cannot be
// asked to fail on cue.
var (
	setServiceAddr = service.SetAddr
	startFresh     = service.StartFresh
	answersWithin  = waitAnswers
)

// restartAndVerify is the helper's half: restart, then check the new port
// answers, and roll back to fallback if it does not.
func restartAndVerify(mgr service.Manager, dataDir, verify, fallback string) error {
	result := api.PortChange{From: portOf(fallback), To: portOf(verify), At: time.Now().Unix()}
	restartErr := mgr.Restart(serviceRestartTimeout)
	if restartErr == nil && answersWithin(verify, portAnswerWithin) {
		result.OK = true
		return api.WritePortChange(dataDir, result)
	}

	// Rolled back. The reason is said in the words a person needs: which port
	// did not work, and that the old one is back.
	result.Error = fmt.Sprintf("the server did not answer on port %s, so it went back to port %s", result.To, result.From)
	if restartErr != nil {
		result.Error += " (" + restartErr.Error() + ")"
	}
	if err := setServiceAddr(fallback); err != nil {
		result.Error += "; putting the old port back failed too: " + err.Error()
		_ = api.WritePortChange(dataDir, result)
		return err
	}
	_ = api.WritePortChange(dataDir, result)
	return startFresh(serviceRestartTimeout)
}

func waitAnswers(addr string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if desktop.ServerRunning(addr) {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func portOf(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return port
	}
	return addr
}

// serviceAddr is where the installed service listens, for the commands that
// must check it is stopped before touching its database: ":8080" unless the
// port has been changed (ADR 0089). Checking the default while the service
// answers on another port would read as "stopped" with the database open.
func serviceAddr() string {
	if a, ok := service.ConfiguredAddr(); ok {
		return a
	}
	return ":8080"
}
