package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"lancast/internal/clientwindow"
	"lancast/internal/knownserver"
)

/*
 * The bindings behind "connect to another server" (ADR 0070).
 *
 * Two rules from the games bindings carry over unchanged, for the same
 * reasons, and one is new.
 *
 * **The page never supplies a pin.** It sends an address; this process fetches
 * the key at that address, compares it with the record, and decides. A page
 * that could hand in a pin could hand in the attacker's, and the trust record
 * would be a formality.
 *
 * **The decision lives in the process.** A mismatch is refused here, not by a
 * dialog choosing not to offer a button. The page cannot connect to a server
 * whose key has changed, however it is asked.
 *
 * **Accepting is an act, and it is separate from probing.** lancastServerProbe
 * looks and reports; lancastServerAccept stores. Nothing stores a pin as a
 * side effect of showing it, because "we already trusted it by the time you
 * read the fingerprint" is not trust on first use.
 */

// probeTimeout bounds a look at an address somebody just typed. Longer than
// the dialer's own timeout would mean a wrong address spends longer being
// wrong than the person is willing to wait.
const probeTimeout = 8 * time.Second

// serverBindings is everything the picker page can call. The window is needed
// because choosing a server ends this one.
func (l *launcher) serverBindings(win func() clientwindow.Controller) map[string]any {
	dir := clientDataDir()

	return map[string]any{
		/*
		 * lancastServers is what the picker draws: every remembered server,
		 * plus the one on this machine.
		 *
		 * The local server is reported as its own thing rather than as a row
		 * in the list, because it is trusted differently -- off disk -- and a
		 * list that mixed the two would invite a "forget" button beside an
		 * entry that has nothing to forget.
		 */
		"lancastServers": func() map[string]any {
			known, err := knownserver.Load(dir)
			out := map[string]any{
				"current": l.target.address,
				"local": map[string]any{
					"address": l.localAddr,
					"current": l.target.local,
				},
				"servers": serverRows(known, l.target.address),
			}
			if err != nil {
				// Said out loud. A trust record that would not parse means
				// every server is about to be asked about again, and a
				// fingerprint prompt nobody can explain is the one that gets
				// clicked through.
				out["error"] = err.Error()
			}
			return out
		},

		/*
		 * lancastServerProbe looks at an address and reports what is there.
		 *
		 * It stores nothing. Its whole output is what a person needs in order
		 * to answer the question: which key, who it claims to be, and what the
		 * record already says about that address.
		 */
		"lancastServerProbe": func(raw string) map[string]any {
			addr, err := knownserver.ParseAddress(raw)
			if err != nil {
				return map[string]any{"error": err.Error()}
			}
			ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
			defer cancel()

			offered, err := knownserver.Fetch(ctx, addr)
			if err != nil {
				return map[string]any{"address": addr, "error": err.Error()}
			}
			known, loadErr := knownserver.Load(dir)
			if loadErr != nil {
				slog.Warn("known servers unreadable while probing", "error", loadErr)
			}

			trust := known.Check(addr, offered.Pin)
			out := map[string]any{
				"address":     addr,
				"fingerprint": knownserver.Fingerprint(offered.Pin),
				"subject":     offered.Subject,
				"expires":     offered.NotAfter.Format(time.RFC3339),
				"trust":       trust.String(),
			}
			if trust == knownserver.TrustMismatch {
				// The sentence travels with the finding, so the page never has
				// to compose it -- and so a refusal reads identically wherever
				// it is met.
				out["refusal"] = mismatchMessage(addr)
			}
			if prev, ok := known.Find(addr); ok {
				out["known_name"] = prev.Name
				out["known_since"] = prev.Accepted.Format(time.RFC3339)
				// The old fingerprint, so a mismatch can be compared rather
				// than merely asserted. Somebody who reinstalled their server
				// is entitled to see that this is what that looks like.
				out["known_fingerprint"] = knownserver.Fingerprint(prev.Pin)
			}
			return out
		},

		/*
		 * lancastServerAccept stores the key currently at an address, then
		 * connects.
		 *
		 * It re-fetches rather than trusting anything the page passes back.
		 * The page has just shown a fingerprint to a person, but what it hands
		 * over is a string, and the only pin worth storing is one this process
		 * read from the address itself.
		 *
		 * A mismatch is refused even here. Accept is for an address with no
		 * record; replacing an existing key is deliberately not reachable from
		 * the prompt, because a person looking at a scary screen with an
		 * "accept" button on it will press the button.
		 */
		"lancastServerAccept": func(raw, name string) map[string]any {
			addr, err := knownserver.ParseAddress(raw)
			if err != nil {
				return map[string]any{"error": err.Error()}
			}
			ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
			defer cancel()

			offered, err := knownserver.Fetch(ctx, addr)
			if err != nil {
				return map[string]any{"error": err.Error()}
			}
			known, _ := knownserver.Load(dir)
			if known.Check(addr, offered.Pin) == knownserver.TrustMismatch {
				return map[string]any{"error": mismatchMessage(addr)}
			}

			known = known.Accept(knownserver.Server{
				Address: addr, Name: name, Pin: offered.Pin,
			})
			if err := knownserver.Save(dir, known); err != nil {
				return map[string]any{"error": err.Error()}
			}
			return connect(addr, win)
		},

		/*
		 * lancastServerConnect opens a server already in the record.
		 *
		 * It checks before it switches. The record being present is not
		 * enough: what matters is the key at that address *now*, and this is
		 * the one place that comparison happens before a window is given a
		 * pin.
		 */
		"lancastServerConnect": func(raw string) map[string]any {
			addr, err := knownserver.ParseAddress(raw)
			if err != nil {
				return map[string]any{"error": err.Error()}
			}
			ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
			defer cancel()

			offered, err := knownserver.Fetch(ctx, addr)
			if err != nil {
				return map[string]any{"error": err.Error()}
			}
			known, _ := knownserver.Load(dir)
			switch known.Check(addr, offered.Pin) {
			case knownserver.TrustMatch:
				return connect(addr, win)
			case knownserver.TrustMismatch:
				return map[string]any{"error": mismatchMessage(addr)}
			default:
				return map[string]any{"error": "this server is not one you have accepted"}
			}
		},

		// lancastServerConnectLocal returns to the server on this machine. No
		// probe: it is trusted off disk, which is the stronger check.
		"lancastServerConnectLocal": func() map[string]any {
			return connect("", win)
		},

		/*
		 * lancastServerPicker opens the picker from inside the app.
		 *
		 * The way back. Without it the only route to a different server is a
		 * launch that has already failed to reach one, which would make
		 * "connect to another server" a feature you can only use when
		 * something is broken.
		 */
		"lancastServerPicker": func() map[string]any {
			c := win()
			if c == nil {
				return map[string]any{"error": "no window"}
			}
			c.ShowHTML(pickerPage(""))
			return map[string]any{"ok": true}
		},

		/*
		 * lancastServerForget drops a record.
		 *
		 * The only route from a refused key back to a connection, and
		 * deliberately a separate act from meeting the refusal.
		 */
		"lancastServerForget": func(raw string) map[string]any {
			addr, err := knownserver.ParseAddress(raw)
			if err != nil {
				return map[string]any{"error": err.Error()}
			}
			known, _ := knownserver.Load(dir)
			if err := knownserver.Save(dir, known.Forget(addr)); err != nil {
				return map[string]any{"error": err.Error()}
			}
			return map[string]any{"ok": true}
		},
	}
}

// connect records the choice and restarts onto it, then closes this window.
func connect(address string, win func() clientwindow.Controller) map[string]any {
	if err := switchTo(address); err != nil {
		return map[string]any{"error": err.Error()}
	}
	if c := win(); c != nil {
		// After the successor has started, so a failure to start leaves the
		// person looking at a window rather than at nothing.
		c.Close()
	}
	return map[string]any{"ok": true}
}

func serverRows(l knownserver.List, current string) []map[string]any {
	rows := make([]map[string]any, 0, len(l.Servers))
	for _, s := range l.Servers {
		rows = append(rows, map[string]any{
			"address":  s.Address,
			"name":     s.Name,
			"accepted": s.Accepted.Format(time.RFC3339),
			"current":  s.Address == current,
		})
	}
	return rows
}

/*
 * mismatchMessage is the refusal, and it is written out here rather than in
 * the page because it is the sentence this whole feature is judged by.
 *
 * It says what changed, what that means, and what the two honest explanations
 * are -- in that order, because somebody who has just reinstalled their server
 * needs to recognise themselves in it immediately, and somebody who has not
 * needs to not be reassured.
 */
func mismatchMessage(addr string) string {
	return fmt.Sprintf(
		"The key at %s is not the one you accepted.\n\n"+
			"LANcast pins a server's public key, and that key does not change when "+
			"a certificate is renewed or when the server gains a network address. "+
			"So this means the key itself was replaced.\n\n"+
			"If the server was reinstalled, or its data directory was recreated, "+
			"that is the explanation -- forget this server and add it again, after "+
			"checking with whoever runs it.\n\n"+
			"If nothing like that happened, something on the network is answering "+
			"in its place, and you should not connect.", addr)
}

// withServerBindings merges the picker's bindings into the window's.
//
// A named function rather than a loop at the call site so the collision rule
// is stated once: the server bindings are added to the desktop ones, and a
// name defined by both is a mistake worth failing on rather than resolving by
// map order.
func withServerBindings(base, extra map[string]any) map[string]any {
	for name, fn := range extra {
		if _, taken := base[name]; taken {
			panic("client: two bindings named " + name)
		}
		base[name] = fn
	}
	return base
}
