package msc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
)

func TestTheEmbeddedModuleIsThePinnedBuild(t *testing.T) {
	wasm, err := inflate()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(wasm)
	if got := hex.EncodeToString(sum[:]); got != ModuleSHA256 {
		t.Fatalf("msc.wasm.gz inflates to sha256 %s, want %s: rebuild with build/build.sh and update README.md, or restore the file", got, ModuleSHA256)
	}
	if len(wasm) != 1225443 {
		t.Errorf("msc.wasm is %d bytes, README.md says 1,225,443", len(wasm))
	}
}

/*
 * The module's whole reach. If a rebuild ever imports path_open, or anything
 * outside WASI, this fails before the server runs it: the module could then
 * name files, and the claim that it cannot would have become untrue without
 * anyone deciding it should.
 */
func TestTheModuleImportsOnlyStreamsAClockAndExit(t *testing.T) {
	ctx := context.Background()
	wasm, err := inflate()
	if err != nil {
		t.Fatal(err)
	}
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	cm, err := rt.CompileModule(ctx, wasm)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range cm.ImportedFunctions() {
		mod, name, _ := f.Import()
		got = append(got, mod+"."+name)
	}
	if len(cm.ImportedMemories()) != 0 {
		t.Error("the module imports a memory; it should own its own")
	}
	sort.Strings(got)
	want := []string{}
	for _, n := range []string{"args_get", "args_sizes_get", "clock_time_get", "environ_get",
		"environ_sizes_get", "fd_close", "fd_fdstat_get", "fd_prestat_dir_name", "fd_prestat_get",
		"fd_read", "fd_seek", "fd_write", "proc_exit"} {
		want = append(want, "wasi_snapshot_preview1."+n)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("imports\n got %v\nwant %v", got, want)
	}
}
