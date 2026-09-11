//go:build wv2harness

/*
 * A WebView2 window pointed at any URL, for reproducing what only the desktop
 * client's engine does.
 *
 * Built because the file HLS path fails in LANcast's window on every film and
 * plays in Chrome 152 and Edge 152 from the same bytes. The only engine that
 * reproduces it is WebView2, and the only WebView2 an agent could previously
 * reach was the user's own running client. This uses the same clientwindow
 * code, with a separate profile directory, so it never touches that client's
 * cookies, storage or single-instance lock.
 *
 * Behind a build tag like devseed and hlsharness: a diagnostic that ships is a
 * diagnostic somebody runs by accident.
 *
 *   go build -tags wv2harness -o wv2harness.exe ./cmd/wv2harness
 *   ./wv2harness.exe -url http://127.0.0.1:8103/test.html -data ./profile -seconds 45
 *
 * WebView2Loader.dll must sit beside the executable.
 */
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"lancast/internal/clientwindow"
)

func main() {
	url := flag.String("url", "", "page to open")
	data := flag.String("data", "", "profile directory for this window (never the client's)")
	seconds := flag.Int("seconds", 45, "close the window after this long")
	flag.Parse()
	if *url == "" || *data == "" {
		fmt.Fprintln(os.Stderr, "need -url and -data")
		os.Exit(2)
	}
	// A test page cannot press play. Only set when nothing else has, so a caller
	// adding its own switches is not silently overwritten.
	const argsEnv = "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"
	if os.Getenv(argsEnv) == "" {
		_ = os.Setenv(argsEnv, "--autoplay-policy=no-user-gesture-required")
	}

	err := clientwindow.Open(clientwindow.Options{
		URL:     *url,
		Title:   "LANcast WebView2 harness",
		Width:   960,
		Height:  600,
		DataDir: *data,
		OnReady: func(c clientwindow.Controller) {
			go func() {
				time.Sleep(time.Duration(*seconds) * time.Second)
				c.Close()
			}()
		},
	})
	fmt.Println("window closed:", err)
}
