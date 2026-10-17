package serverapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lancast/internal/api"
	"lancast/internal/service"
)

/*
 * The restart after a port change (ADR 0089): the new port answers and the
 * outcome is written, or it does not and the old port goes back. The service
 * manager is faked because the rollback is the part worth proving and the one
 * a real one cannot be made to need on cue.
 */

type restartOnly struct {
	service.Manager
	restarts int
}

func (r *restartOnly) Restart(time.Duration) error { r.restarts++; return nil }

func fakeService(t *testing.T, answers bool) (set *[]string, fresh *int) {
	t.Helper()
	var addrs []string
	n := 0
	oldSet, oldFresh, oldAns := setServiceAddr, startFresh, answersWithin
	setServiceAddr = func(a string) error { addrs = append(addrs, a); return nil }
	startFresh = func(time.Duration) error { n++; return nil }
	answersWithin = func(string, time.Duration) bool { return answers }
	t.Cleanup(func() { setServiceAddr, startFresh, answersWithin = oldSet, oldFresh, oldAns })
	return &addrs, &n
}

func readResult(t *testing.T, dir string) api.PortChange {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "port-change.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r api.PortChange
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestANewPortThatAnswersIsKept(t *testing.T) {
	set, fresh := fakeService(t, true)
	dir := t.TempDir()
	mgr := &restartOnly{}
	if err := restartAndVerify(mgr, dir, ":9090", ":8080"); err != nil {
		t.Fatal(err)
	}
	if mgr.restarts != 1 || len(*set) != 0 || *fresh != 0 {
		t.Errorf("restarts %d, addresses set %v, fresh starts %d", mgr.restarts, *set, *fresh)
	}
	if r := readResult(t, dir); !r.OK || r.From != "8080" || r.To != "9090" {
		t.Errorf("result = %+v", r)
	}
}

// The failure this exists for: a server that cannot listen on its new port
// must not be left off.
func TestANewPortThatDoesNotAnswerIsRolledBack(t *testing.T) {
	set, fresh := fakeService(t, false)
	dir := t.TempDir()
	if err := restartAndVerify(&restartOnly{}, dir, ":9090", ":8080"); err != nil {
		t.Fatal(err)
	}
	if len(*set) != 1 || (*set)[0] != ":8080" || *fresh != 1 {
		t.Errorf("addresses set %v, fresh starts %d; want :8080 put back and one start", *set, *fresh)
	}
	r := readResult(t, dir)
	if r.OK || !strings.Contains(r.Error, "did not answer on port 9090") || !strings.Contains(r.Error, "back to port 8080") {
		t.Errorf("result = %+v", r)
	}
}
