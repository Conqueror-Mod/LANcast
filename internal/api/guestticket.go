package api

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"lancast/internal/guestticket"
	"lancast/internal/identity"
	"lancast/internal/store"
)

/*
 * Minting a ticket for one of this server's own people.
 *
 * This is the *asking* side of [ADR 0046](../../docs/adr/0046-remote-guests.md)
 * §2: Georgia's client asks Georgia's server for a ticket for peer Chris, and
 * her server signs it with its identity key. Chris's server is not involved and
 * is not contacted — it verifies the signature when the ticket arrives, against
 * a key it already pinned at pairing.
 *
 * That separation is the point of the whole design. No password crosses, no
 * account is created on the far side, and each household governs its own
 * people: Chris never administers Georgia's, and his user list stays his
 * household rather than a list of everybody he has watched a film with.
 */

/*
 * guestTicketTTL is how long a minted ticket is good for.
 *
 * Long enough to survive a clock a minute out (guestticket.Skew) and a round
 * trip to a server that may be waking a disk. Short enough that a ticket
 * captured in transit is worth little by the time anybody has it, and that the
 * nonce store's memory is bounded by minutes rather than hours.
 *
 * It is not a session lifetime. Redemption trades this for whatever the far
 * server decides to issue, and that decision belongs to the far server.
 */
const guestTicketTTL = 2 * time.Minute

/*
 * Which of this server's people may ask, and the policy is deliberate.
 *
 * Any signed-in account may ask for a ticket **naming itself**. It cannot ask
 * for one naming somebody else, which is the only rule that has to hold here:
 * a ticket is an assertion about who is asking, and letting one person mint a
 * ticket in another's name would make the subject meaningless on the far side.
 *
 * Beyond that this server does not restrict. A share is granted to a *server*
 * (ADR 0071 §1), so what a friend's household may see is a decision the
 * granting household already made about the household, not about each person
 * in it. Adding a per-person gate here would be a second, weaker copy of a
 * control that already exists on the other side, and one the granting host
 * could not see or rely on.
 *
 * The case for changing it later is a household that wants its own children
 * limited on somebody else's library. That is a real want, it belongs to the
 * *asking* server, and it would be a ceiling applied here at mint time. It is
 * not this change.
 */
func (s *Server) mintGuestTicket(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return
	}

	fingerprint := identity.Normalize(r.PathValue("fingerprint"))
	if fingerprint == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which server is this ticket for")
		return
	}

	/*
	 * Paired, not merely added. A pairing that has not completed has no
	 * confirmed key on either side, so a ticket for it could not be verified
	 * and handing one out would be inviting somebody to a door that cannot
	 * open — reported as a failure at the far end, where it is hardest to
	 * diagnose.
	 */
	p, err := s.st.PeerByFingerprint(r.Context(), fingerprint)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such peer")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "peer for ticket")
		return
	}
	if p.State != store.PeerPaired {
		writeError(w, http.StatusConflict, "not_paired",
			"this pairing is not complete, so a ticket for it could not be verified")
		return
	}

	nonce, err := newNonce()
	if err != nil {
		s.writeInternal(w, err, "ticket nonce")
		return
	}

	now := time.Now()
	tok, err := guestticket.Mint(s.ident.Signer(), guestticket.Claims{
		Issuer:   s.ident.Fingerprint(),
		Subject:  sess.UserID,
		Audience: p.Fingerprint,
		Nonce:    nonce,
		IssuedAt: now,
		Expires:  now.Add(guestTicketTTL),
	})
	if err != nil {
		s.writeInternal(w, err, "mint ticket")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":     tok,
		"expires_at": now.Add(guestTicketTTL).Unix(),
		"peer":       p.Fingerprint,
	})
}

/*
 * newNonce is 128 bits from crypto/rand.
 *
 * It only has to be unguessable and not repeat within a ticket's lifetime, and
 * a counter would do the second without the first. Unguessable matters because
 * a nonce somebody can predict is a nonce they can spend first, which turns
 * the replay defence into a way to refuse a legitimate admission.
 */
func newNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
