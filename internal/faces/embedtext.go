package faces

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

/*
 * The text embedder, kept warm between searches (ADR 0060, amended).
 *
 * WHY THIS EXISTS
 *
 * The first version started a process per query. Measured, that is about 1.2s
 * of loading a 250MB model to do a few milliseconds of arithmetic — paid on
 * every search, at every library size, for ever. On the library this was built
 * against it was roughly six times the cost of searching the photographs.
 *
 * ADR 0060 named this fallback before it was needed: "if it proves too slow the
 * answer is a long-lived worker process, not a cached vector — caching would
 * key on a string somebody typed and the first typo would fill it with nonsense
 * nobody could clear." It proved too slow. This is that worker.
 *
 * THE PROTOCOL IS ORDER, WHICH IS WHY EVERYTHING IS SERIALIZED
 *
 * One query per line in, one JSON line out, matched by *position* — there are
 * no request ids. That is the same shape `detect` and `embed` already use, and
 * it is only safe while exactly one caller is mid-exchange, so every send holds
 * the mutex through reading its own answer.
 *
 * The failure this prevents is not a crash. A desynchronised pipe answers each
 * question with the previous question's vector: every search then returns a
 * confident, plausible, correctly-ranked list of photographs for something
 * nobody asked, and nothing anywhere reports a fault. Two things guard it — the
 * lock, and the worker's promise to emit a line even for a query it could not
 * embed.
 *
 * IT DOES NOT STAY RESIDENT FOR EVER
 *
 * A media server that holds 250MB permanently for a feature somebody uses twice
 * a month has taken something that is not its to take. So the process exits
 * after a spell of quiet, and a burst of searching — which is how searching
 * actually happens — pays the load once at the start of it rather than once per
 * query.
 */

// textIdle is how long the worker waits for another query before exiting. Long
// enough that a session of searching stays warm throughout, short enough that
// an afternoon of not searching costs nothing.
const textIdle = 5 * time.Minute

// textEmbedder owns one long-lived `embed-text` process.
type textEmbedder struct {
	tool *Tool

	mu    sync.Mutex
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *bufio.Scanner
	timer *time.Timer
}

/*
 * EmbedText turns a typed query into a vector in the same space.
 *
 * Retried once against a fresh process, and that is not defensive padding: the
 * worker may have exited between two searches — its own idle timer, an operator
 * with a task manager, a segfaulting vision library — and the first write to a
 * dead pipe is how this end finds out. Failing that search would make the
 * feature look broken at exactly the moment it was about to work.
 *
 * The retry is bounded at one, because a worker that cannot start twice in a
 * row is not a race, it is an install.
 */
func (t *Tool) EmbedText(ctx context.Context, query string) ([]float32, error) {
	/*
	 * A newline in the query would end it early and leave the rest of it
	 * looking like the *next* question, which the next search would then be
	 * answered with. Collapsed rather than rejected: a person who pasted two
	 * lines into a search box asked one question.
	 */
	query = strings.Join(strings.Fields(query), " ")
	if query == "" {
		return nil, fmt.Errorf("embed query: empty")
	}

	te := t.text()
	v, err := te.ask(ctx, query)
	if err == nil {
		return v, nil
	}
	/*
	 * Only a broken pipe is retried, and telling the two apart is the point.
	 *
	 * A worker that *answered* and said it could not embed this query has left
	 * the stream in step and has nothing wrong with it. Restarting it would pay
	 * the 1.2s model load again to be told the same thing — on every
	 * unluckily-worded search, which is precisely the cost this whole file
	 * exists to remove. The test that caught it counted the processes.
	 */
	var qe queryError
	if errors.As(err, &qe) {
		return nil, err
	}
	te.stop()
	return te.ask(ctx, query)
}

/*
 * queryError is the worker declining one query, as opposed to the pipe failing.
 *
 * A distinct type rather than a string check: this decides whether a 250MB
 * process is torn down and rebuilt, and matching on message text would make
 * that decision depend on wording nobody thinks of as load-bearing.
 */
type queryError struct{ msg string }

func (e queryError) Error() string { return "embed query: " + e.msg }

func (t *Tool) text() *textEmbedder {
	t.textOnce.Do(func() { t.textEmb = &textEmbedder{tool: t} })
	return t.textEmb
}

func (te *textEmbedder) ask(ctx context.Context, query string) ([]float32, error) {
	te.mu.Lock()
	defer te.mu.Unlock()

	if err := te.startLocked(); err != nil {
		return nil, err
	}
	// Pushed out on every query rather than set once: the worker should live
	// for five minutes after the *last* search, not after the first.
	te.timer.Reset(textIdle)

	if _, err := io.WriteString(te.stdin, query+"\n"); err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if !te.out.Scan() {
		if err := te.out.Err(); err != nil {
			return nil, fmt.Errorf("embed query: %w", err)
		}
		return nil, fmt.Errorf("embed query: the worker stopped answering")
	}

	var line embedLine
	if err := json.Unmarshal(te.out.Bytes(), &line); err != nil {
		/*
		 * An unreadable line means the stream is no longer a sequence of
		 * answers, and there is no way to tell how far it has slipped. The pipe
		 * is not recoverable by skipping — the caller kills it, which is the
		 * only response that cannot answer a later question with an earlier
		 * vector.
		 */
		return nil, fmt.Errorf("embed query: unreadable answer: %w", err)
	}
	/*
	 * The answer must name the question it answered.
	 *
	 * This is not belt and braces; it is the guard for a failure that shipped.
	 * A worker one version behind the server was left in place beside it, did
	 * not understand stdin mode, read `-q ""`, embedded the *empty string*,
	 * printed one vector and exited — and every search was then ranked against
	 * that. The results were ordered, plausible, and about nothing anybody had
	 * typed, and nothing raised so much as a warning.
	 *
	 * Treated as a pipe failure rather than a query failure, so the caller
	 * restarts once and then reports honestly. A worker that cannot name what
	 * it answered is not one to keep asking.
	 */
	if line.Path != query {
		return nil, fmt.Errorf(
			"embed query: the worker answered for %q, not %q — it is probably "+
				"older than this server", line.Path, query)
	}
	if line.Error != "" {
		// The worker answered, and said it could not do this one. The pipe is
		// still in step, so this is the query's failure and not the process's.
		return nil, queryError{line.Error}
	}
	if len(line.Vector) == 0 {
		// Also an answer, and also in step — an empty vector is a line the
		// worker chose to send.
		return nil, queryError{"the worker returned no vector"}
	}
	return line.Vector, nil
}

// startLocked brings the worker up if it is not already. The caller holds mu.
func (te *textEmbedder) startLocked() error {
	if te.cmd != nil {
		return nil
	}
	path, ok := te.tool.Path()
	if !ok {
		return fmt.Errorf("the worker is not installed")
	}

	/*
	 * context.Background, not the request's.
	 *
	 * The process outlives the search that started it — that is the entire
	 * point — so binding it to a request context would kill the worker the
	 * moment that one search returned, and the next search would pay the model
	 * load again. It is stopped by the idle timer and by Forget instead.
	 */
	cmd := exec.CommandContext(context.Background(), path, "embed-text",
		"-models", te.tool.ModelsDir)
	cmd.Env = te.tool.env()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	sc := bufio.NewScanner(stdout)
	// A 512-float vector as JSON text is comfortably past the default 64KB.
	sc.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)

	te.cmd, te.stdin, te.out = cmd, stdin, sc
	te.timer = time.AfterFunc(textIdle, te.stop)
	return nil
}

/*
 * stop shuts the worker down and forgets it, so the next query starts a fresh
 * one.
 *
 * Closing stdin rather than killing: the worker's loop ends when stdin closes,
 * which lets it release the model the way it would on any other exit. Wait then
 * reaps it — without that a server that searched for a month would leave a
 * month of zombies behind it.
 */
func (te *textEmbedder) stop() {
	te.mu.Lock()
	cmd, stdin, timer := te.cmd, te.stdin, te.timer
	te.cmd, te.stdin, te.out, te.timer = nil, nil, nil, nil
	te.mu.Unlock()

	if timer != nil {
		timer.Stop()
	}
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd == nil {
		return
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// It did not leave when asked. A vision library can wedge inside a
		// native call, and a stuck worker holding 250MB is worse than an
		// ungraceful one.
		_ = cmd.Process.Kill()
		<-done
	}
}

// StopText shuts the warm worker down. Called when the models change, because
// a process that loaded the old ones would go on answering in their coordinate
// system long after the database had moved to another.
func (t *Tool) StopText() {
	t.textOnce.Do(func() { t.textEmb = &textEmbedder{tool: t} })
	t.textEmb.stop()
}
