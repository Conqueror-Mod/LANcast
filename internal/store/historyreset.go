package store

import (
	"context"
	"fmt"
)

/*
 * Forgetting what somebody watched.
 *
 * Beside history.go rather than in it: that file *reads* playback_state to
 * answer "what have I watched", and this one deletes it. Keeping the
 * destructive half in its own file is not tidiness — it is so that a reader
 * looking for the thing that removes viewing records finds one file containing
 * only that, rather than two functions buried among the ones that count.
 *
 * There is no undo and there should not be one. A reset that keeps a copy is
 * not a reset, and the reasons people ask for it — a shared account watched
 * something for somebody else, a server changing hands, a film that autoplayed
 * overnight — are all "make the record gone", not "hide it".
 */

// HistoryScope is what a reset is allowed to touch.
type HistoryScope string

const (
	// HistoryAll forgets every playback record for this account.
	HistoryAll HistoryScope = "all"
	/*
	 * HistoryFinished forgets what was finished and leaves positions alone.
	 *
	 * The distinction exists because `playback_state` is one table carrying two
	 * meanings, and somebody asking to forget a show they finished rarely means
	 * "and lose my place in the one I am half way through". A single button
	 * cannot serve both, so there are three.
	 */
	HistoryFinished HistoryScope = "finished"
	// HistoryUnfinished forgets positions and keeps the watched flags — for the
	// film that autoplayed at 3am. The mirror of the above, free once the split
	// exists.
	HistoryUnfinished HistoryScope = "unfinished"
)

// scopeClause turns a scope into SQL, or refuses.
//
// Shared by the count and the delete so they can never disagree about what a
// scope means — a preview that prices one thing and a delete that removes
// another is the worst possible bug in an irreversible action.
func scopeClause(scope HistoryScope) (string, error) {
	switch scope {
	case HistoryAll:
		return "", nil
	case HistoryFinished:
		return ` AND watched = 1`, nil
	case HistoryUnfinished:
		return ` AND watched = 0`, nil
	}
	return "", fmt.Errorf("unknown history scope %q", scope)
}

/*
 * underClause narrows to one item and everything beneath it.
 *
 * So "forget this show" is one call rather than a client walking seasons and
 * episodes issuing a delete per row. Recursive over `parent_id`, which is how
 * every container in this schema holds its children — show → season → episode,
 * album → track — because a show's watched state lives on its episodes and
 * forgetting the parent alone would leave the rows that actually record the
 * watching.
 *
 * Bounded by the hierarchy rather than a depth literal, so a container kind
 * added later needs nothing here.
 */
const underClause = ` AND item_id IN (
		WITH RECURSIVE tree(id) AS (
			SELECT ?
			UNION ALL
			SELECT m.id FROM media_item m JOIN tree t ON m.parent_id = t.id
		)
		SELECT id FROM tree
	)`

func historyWhere(userID string, scope HistoryScope, under int64) (string, []any, error) {
	clause, err := scopeClause(scope)
	if err != nil {
		return "", nil, err
	}
	args := []any{userID}
	if under > 0 {
		clause += underClause
		args = append(args, under)
	}
	return clause, args, nil
}

/*
 * ResetHistory deletes this account's playback state and reports how much went.
 *
 * Per account, never global, and there is deliberately no user-id parameter for
 * a caller to supply: playback state is keyed by user (ADR 0006) precisely so
 * that one person's viewing is their own, and an administrator clearing their
 * own history must not be able to reach into anybody else's. The session
 * decides, one layer up.
 */
func (s *Store) ResetHistory(ctx context.Context, userID string, scope HistoryScope, under int64) (int64, error) {
	where, args, err := historyWhere(userID, scope, under)
	if err != nil {
		return 0, fmt.Errorf("reset history: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM playback_state WHERE user_id = ?`+where, args...)
	if err != nil {
		return 0, fmt.Errorf("reset history: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reset history: %w", err)
	}
	return n, nil
}

/*
 * HistoryCount prices a reset without performing it.
 *
 * So the confirmation can say "this forgets 412 things" rather than "are you
 * sure". A number is what makes an irreversible action reviewable: somebody who
 * expected to clear one show and is told four hundred has learned something
 * while it is still free.
 */
func (s *Store) HistoryCount(ctx context.Context, userID string, scope HistoryScope, under int64) (int64, error) {
	where, args, err := historyWhere(userID, scope, under)
	if err != nil {
		return 0, fmt.Errorf("history count: %w", err)
	}
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM playback_state WHERE user_id = ?`+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("history count: %w", err)
	}
	return n, nil
}
