package main

import (
	"lancast/internal/knownserver"
)

/*
 * Which server this window opens, and why switching one costs a relaunch.
 *
 * The client pins the server's key with Chromium's
 * --ignore-certificate-errors-spki-list, passed through
 * WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS, and that variable is **read when the
 * web view environment is created**. A window that is already open cannot be
 * re-pinned. So "connect to a different server" is not a navigation; it is a
 * new process with a different pin.
 *
 * That is not a workaround, and the alternative is worth naming because it
 * looks cheaper. The switch takes a comma-separated *list*, so every known
 * server's key could be pinned at startup and switching would be a mere
 * Navigate -- at the cost that the window would then accept any trusted key at
 * any address, because Chromium's list is not scoped to a host. The identity
 * this client keeps is "this key, at this address"
 * ([ADR 0070](../../docs/adr/0070-the-desktop-client-can-trust-a-server-it-did-not-install.md));
 * a list quietly widens it to "any key we have ever trusted, anywhere", which
 * is a different and weaker claim. One key per window keeps the pin saying
 * what the record says.
 *
 * Everything in this file is pure. Which server to open is a decision made
 * from facts gathered elsewhere, so it can be tested as cases rather than by
 * arranging a second machine.
 */

// target is the server a window is about to open.
type target struct {
	// address is canonical host:port.
	address string
	// local marks the server on this machine, which is resolved and trusted
	// the way it always has been: the pin is read off local disk, which is a
	// stronger check than anything over a network, and this process may start
	// or wait for it.
	local bool
	// pin is the key to trust, empty when the server needs none -- a
	// loopback-only server serves plain HTTP and has no certificate.
	pin string
	// name is what the person called this server. Cosmetic.
	name string
}

/*
 * facts is everything resolve needs, gathered by the caller.
 *
 * A struct rather than six arguments because each field is a question with a
 * real answer somewhere in the process, and a bare bool at a call site is how
 * "the operator chose this address" and "this is the default address" end up
 * looking identical.
 */
type facts struct {
	// addrFlagGiven is whether an operator actually typed -addr, as opposed to
	// the flag carrying its default. The difference matters: an explicit -addr
	// is an instruction, and this file must not second-guess it.
	addrFlagGiven bool
	// flagAddr is the value of -addr, default or not.
	flagAddr string
	// lastServer is the address this client opened last, empty for the local
	// server. It is a preference, not a permission: it is only honoured if the
	// trust record still holds that server.
	lastServer string
	// known is the trust record.
	known knownserver.List
	// localPin is the pin read off this machine's data directory, empty when
	// there is no certificate there.
	localPin string
}

/*
 * resolve decides which server to open, and whether to ask instead.
 *
 * Asking is the second return. It is true only when the remembered choice
 * cannot be honoured -- which is a deliberate narrowing: this client opens
 * where it opened last time without a question, because a picker that appears
 * every launch is a picker people stop reading.
 */
func resolve(f facts) (t target, ask bool) {
	/*
	 * An explicit -addr wins outright and keeps today's behaviour.
	 *
	 * This is the operator's escape hatch and the path every existing install
	 * and every test takes. Treating it as a *remote* server would mean a
	 * service on a non-default port suddenly needed a trust prompt for a
	 * machine it is running on.
	 */
	if f.addrFlagGiven {
		return target{address: f.flagAddr, local: true, pin: f.localPin}, false
	}

	// Nothing remembered: the server on this machine, exactly as before.
	if f.lastServer == "" {
		return localTarget(f), false
	}

	addr, err := knownserver.ParseAddress(f.lastServer)
	if err != nil {
		// A preference that will not parse is a preference about nothing.
		return localTarget(f), false
	}

	known, ok := f.known.Find(addr)
	if !ok {
		/*
		 * Remembered, but no longer trusted.
		 *
		 * This is what forgetting a server looks like on the next launch, and
		 * it is the one case worth interrupting for: opening the local server
		 * silently would look like the remote one had been lost, and opening
		 * the remote one is exactly what forgetting was supposed to prevent.
		 */
		return localTarget(f), true
	}
	return target{address: addr, pin: known.Pin, name: known.Name}, false
}

// localTarget is the server on this machine.
func localTarget(f facts) target {
	return target{address: f.flagAddr, local: true, pin: f.localPin}
}
