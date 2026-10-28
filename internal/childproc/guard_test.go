package childproc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
 * Every process LANcast starts goes through this package.
 *
 * LANcast.exe is linked -H=windowsgui and has no console, so a console
 * program it starts (ffmpeg, ffprobe, lancast-faces) gets a visible console
 * window of its own unless it is hidden. Credits and intro detection started
 * ffmpeg without Hide for months, and so did the face worker: on a server
 * not running as the service, a command window flashed up for every file
 * they read. Nothing failed, because a flashing window is not an error.
 *
 * So this reads the source: a function that calls exec.Command or
 * exec.CommandContext must also call into childproc (Hide or Detach). The
 * exceptions are named, each with its reason.
 */
var notConsolePrograms = map[string]string{
	// Starts LANcast itself (or the client), a windowed program: no console
	// to hide, and Hide would hide the window it exists to show.
	"serverapp/tray_windows.go:trayRun": "starts a windowed program",
	// Linux only: systemctl from a service manager, no windows to flash.
	"service/manager_linux.go:Status":    "Linux only",
	"service/manager_linux.go:systemctl": "Linux only",
	// A developer's command-line lab, run from a terminal.
	"marker/introlab/main.go:decodeHead": "developer tool, run from a terminal",
}

func TestEveryProcessIsStartedHidden(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "childproc/") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			return nil
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			starts, hidden := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				if pkg.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") {
					starts = true
				}
				if pkg.Name == "childproc" {
					hidden = true
				}
				return true
			})
			if !starts || hidden {
				continue
			}
			key := rel + ":" + fn.Name.Name
			if _, ok := notConsolePrograms[key]; ok {
				seen[key] = true
				continue
			}
			t.Errorf("%s starts a process without childproc.Hide: a console window will flash on Windows", key)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// An exception that no longer matches anything is a stale excuse.
	for key := range notConsolePrograms {
		if !seen[key] {
			t.Errorf("exception %s matches nothing any more; remove it", key)
		}
	}
}
