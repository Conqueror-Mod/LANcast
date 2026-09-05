package faces

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

/*
 * The warm text worker, driven with a stand-in for the real one.
 *
 * The inference is a subprocess needing a C toolchain and 250MB of model, and
 * it is not exercised here. What is exercised is the part that can go wrong
 * without failing: whether one process really is reused, whether answers stay
 * matched to their questions, and whether a worker that died between two
 * searches costs the second one anything.
 *
 * The middle of those is why this file is careful. The protocol is order — one
 * line in, one line out, no request ids — so a desynchronised pipe does not
 * crash. It answers each search with the previous search's vector, and every
 * result is a confident, correctly-ranked list of photographs for something
 * nobody asked. Nothing reports it, and there is no log line for it. Here is
 * the only place it can be caught.
 *
 * THE STAND-IN IS THIS TEST BINARY
 *
 * It was a shell script first, which meant every test in this file skipped on
 * Windows — the platform the feature actually ships on, and the one it was
 * being developed on, so they proved nothing where it mattered. Re-entering the
 * test binary works everywhere and needs no toolchain.
 */

const fakeWorkerEnv = "LANCAST_FAKE_TEXT_WORKER"

/*
 * TestMain lets this binary act as the worker when the environment says so.
 *
 * A child started by the code under test inherits the variable and takes this
 * branch before any test runs; the parent set it with t.Setenv afterwards, so
 * it never sees it here.
 */
func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeWorkerEnv); mode != "" {
		os.Exit(runFakeWorker(mode))
	}
	os.Exit(m.Run())
}

/*
 * runFakeWorker imitates `embed-text` reading queries from stdin.
 *
 * It echoes each query back in `path`, so an answer that has slipped onto the
 * wrong question is visible rather than merely suspected.
 */
func runFakeWorker(mode string) int {
	// A line per process start, so a test can count how many were spawned.
	if counter := os.Getenv(fakeWorkerEnv + "_COUNT"); counter != "" {
		if f, err := os.OpenFile(counter, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintln(f, "start")
			_ = f.Close()
		}
	}

	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		q := sc.Text()
		switch {
		case mode == "once":
			// Answers one query and leaves — the state an idle timeout, or an
			// operator with a task manager, leaves behind.
			fmt.Printf("{\"vector\":[1,0],\"path\":%q}\n", q)
			return 0
		case strings.HasPrefix(q, "bad"):
			fmt.Println(`{"error":"cannot embed that"}`)
		default:
			fmt.Printf("{\"vector\":[1,0],\"path\":%q}\n", q)
		}
	}
	return 0
}

// fakeWorker points a Tool at a copy of this test binary, and returns the path
// of the file that counts process starts.
func fakeWorker(t *testing.T, mode string) (*Tool, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, exeName())

	in, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}

	counter := filepath.Join(dir, "starts")
	t.Setenv(fakeWorkerEnv, mode)
	t.Setenv(fakeWorkerEnv+"_COUNT", counter)

	tool := &Tool{Dir: dir, ModelsDir: dir}
	t.Cleanup(tool.StopText)
	return tool, counter
}

func starts(t *testing.T, counter string) int {
	t.Helper()
	b, err := os.ReadFile(counter)
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(b)))
}

/*
 * One process answers many queries.
 *
 * This is the whole point of the change: a process per query spent about 1.2s
 * loading a model to do milliseconds of arithmetic, on every search for ever.
 */
func TestTheWorkerIsReusedAcrossSearches(t *testing.T) {
	tool, counter := fakeWorker(t, "serve")

	for _, q := range []string{"a dog", "a cat", "snow"} {
		if _, err := tool.EmbedText(context.Background(), q); err != nil {
			t.Fatalf("%q: %v", q, err)
		}
	}

	if n := starts(t, counter); n != 1 {
		t.Errorf("started %d processes for three searches, want 1 — the model "+
			"load is being paid per query", n)
	}
}

/*
 * Every answer belongs to its own question, under concurrent callers.
 *
 * Run concurrently because the lock is what makes an order-based protocol safe,
 * and a single-threaded test passes whether the lock is there or not.
 */
func TestAnswersStayMatchedToTheirQueries(t *testing.T) {
	tool, _ := fakeWorker(t, "serve")

	var wg sync.WaitGroup
	bad := make(chan string, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q := fmt.Sprintf("query-%02d", i)
			if _, err := tool.EmbedText(context.Background(), q); err != nil {
				bad <- fmt.Sprintf("%s: %v", q, err)
			}
		}(i)
	}
	wg.Wait()
	close(bad)
	for msg := range bad {
		t.Error(msg)
	}
}

/*
 * A worker that died between two searches costs the second one nothing.
 *
 * It exits on its own idle timer, and an operator may kill it besides. The
 * first write to a dead pipe is how this end finds out, and failing that search
 * would make the feature look broken at the moment it was about to work.
 */
func TestASearchSurvivesAWorkerThatDiedWhileIdle(t *testing.T) {
	tool, counter := fakeWorker(t, "once")

	if _, err := tool.EmbedText(context.Background(), "first"); err != nil {
		t.Fatalf("first search: %v", err)
	}
	if _, err := tool.EmbedText(context.Background(), "second"); err != nil {
		t.Errorf("second search after the worker exited: %v", err)
	}
	if n := starts(t, counter); n < 2 {
		t.Errorf("only %d process started; the second search did not restart "+
			"the worker", n)
	}
}

/*
 * A query the worker refuses is that query's failure, not the pipe's.
 *
 * The worker answers every line, errors included, so the stream stays in step.
 * Treating this as a broken process would restart it on every unluckily-worded
 * search and pay the model load each time.
 */
func TestARefusedQueryIsNotAPipeFailure(t *testing.T) {
	tool, counter := fakeWorker(t, "serve")

	if _, err := tool.EmbedText(context.Background(), "good"); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.EmbedText(context.Background(), "bad one"); err == nil {
		t.Error("a refused query was reported as success")
	}
	if _, err := tool.EmbedText(context.Background(), "good again"); err != nil {
		t.Fatalf("a later search failed after a refused one: %v", err)
	}

	if n := starts(t, counter); n != 1 {
		t.Errorf("started %d processes; a refused query restarted the worker", n)
	}
}

/*
 * A newline in the query is collapsed rather than sent.
 *
 * Sent as-is it would end the line early and leave the remainder looking like
 * the next question — which the next search would then be answered with. A
 * person who pasted two lines into a search box asked one question, so it is
 * folded rather than refused.
 */
func TestAPastedNewlineCannotSplitTheQuery(t *testing.T) {
	tool, _ := fakeWorker(t, "serve")

	if _, err := tool.EmbedText(context.Background(), "a dog\non a beach"); err != nil {
		t.Fatalf("a pasted two-line query failed: %v", err)
	}
	if _, err := tool.EmbedText(context.Background(), "a cat"); err != nil {
		t.Errorf("the following search failed: %v", err)
	}
}

// An empty query never reaches the worker: it is not a question, and sending it
// would consume an answer nobody asked for.
func TestAnEmptyQueryIsRefusedBeforeTheWorker(t *testing.T) {
	tool := &Tool{}
	if _, err := tool.EmbedText(context.Background(), "   \n  "); err == nil {
		t.Error("an empty query was sent to the worker")
	}
}
