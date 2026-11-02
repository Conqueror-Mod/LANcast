package store

import (
	"context"
	"testing"
)

func TestARetroAccountIsConnectedReplacedAndGone(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if a, err := st.GetRetroAccount(ctx, "u_1"); err != nil || a != nil {
		t.Fatalf("before connecting: %+v, %v", a, err)
	}
	if err := st.SetRetroAccount(ctx, RetroAccount{UserID: "u_1", RAUser: "Old", RAULID: "01A", APIKeySealed: []byte{1}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRetroAccount(ctx, RetroAccount{UserID: "u_1", RAUser: "New", RAULID: "01A", APIKeySealed: []byte{2}}); err != nil {
		t.Fatal(err)
	}
	a, _ := st.GetRetroAccount(ctx, "u_1")
	if a == nil || a.RAUser != "New" || a.APIKeySealed[0] != 2 {
		t.Fatalf("after reconnecting: %+v", a)
	}
	if err := st.DeleteRetroAccount(ctx, "u_1"); err != nil {
		t.Fatal(err)
	}
	if a, _ := st.GetRetroAccount(ctx, "u_1"); a != nil {
		t.Error("still connected after disconnecting")
	}
}

// Deleting the LANcast account takes its connection, key and all.
func TestDeletingTheAccountDisconnectsIt(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, "u_kid", "kid", "x", RoleMember); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRetroAccount(ctx, RetroAccount{UserID: "u_kid", RAUser: "Kid", RAULID: "01B", APIKeySealed: []byte{1}}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(ctx, "u_kid"); err != nil {
		t.Fatal(err)
	}
	if a, _ := st.GetRetroAccount(ctx, "u_kid"); a != nil {
		t.Error("a deleted account's RetroAchievements key survived it")
	}
}

func connectRetro(t *testing.T, st *Store, user, ulid string) {
	t.Helper()
	if err := st.SetRetroAccount(context.Background(), RetroAccount{UserID: user, RAUser: "Player", RAULID: ulid, APIKeySealed: []byte{1}}); err != nil {
		t.Fatal(err)
	}
}

func queueOne(t *testing.T, st *Store, user, request string) int64 {
	t.Helper()
	id, err := st.EnqueueRetro(context.Background(), RetroQueued{UserID: user, RAUser: "Player", Request: request, Form: "r=" + request + "&a=1", UserAgent: "LANcast/1 (Windows 10.0) mGBA/1"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Earning (ADR 0112) is a token on the connection: on, off, and only for an
// account that has one.
func TestEarningIsATokenOnTheConnection(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if err := st.SetRetroConnectToken(ctx, "u_1", []byte{9}); err != ErrNoRetroAccount {
		t.Fatalf("earning with no connection: %v, want ErrNoRetroAccount", err)
	}
	connectRetro(t, st, "u_1", "01A")
	if a, _ := st.GetRetroAccount(ctx, "u_1"); a.ConnectTokenSealed != nil {
		t.Fatal("a fresh connection is already earning")
	}
	if err := st.SetRetroConnectToken(ctx, "u_1", []byte{9}); err != nil {
		t.Fatal(err)
	}
	if a, _ := st.GetRetroAccount(ctx, "u_1"); len(a.ConnectTokenSealed) != 1 || a.ConnectTokenSealed[0] != 9 {
		t.Fatalf("token after turning earning on: %v", a.ConnectTokenSealed)
	}
	if err := st.SetRetroConnectToken(ctx, "u_1", nil); err != nil {
		t.Fatal(err)
	}
	if a, _ := st.GetRetroAccount(ctx, "u_1"); a.ConnectTokenSealed != nil {
		t.Fatal("still earning after turning it off")
	}
}

// Reconnecting the same RetroAchievements account keeps earning on; a
// different one does not inherit a token issued to somebody else.
func TestATokenStaysWithTheAccountItWasIssuedFor(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	connectRetro(t, st, "u_1", "01A")
	if err := st.SetRetroConnectToken(ctx, "u_1", []byte{9}); err != nil {
		t.Fatal(err)
	}
	connectRetro(t, st, "u_1", "01A") // a new API key, same account
	if a, _ := st.GetRetroAccount(ctx, "u_1"); a.ConnectTokenSealed == nil {
		t.Fatal("a new API key for the same account turned earning off")
	}
	connectRetro(t, st, "u_1", "01Z") // somebody else's account
	if a, _ := st.GetRetroAccount(ctx, "u_1"); a.ConnectTokenSealed != nil {
		t.Fatal("a different RetroAchievements account inherited the old token")
	}
}

// The queue comes back oldest first, across accounts, and keeps attempts.
func TestTheQueueIsInOrder(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	a := queueOne(t, st, "u_1", "awardachievement")
	b := queueOne(t, st, "u_2", "submitlbentry")
	c := queueOne(t, st, "u_1", "awardachievement")
	if err := st.NoteRetroAttempt(ctx, a, "RetroAchievements is down"); err != nil {
		t.Fatal(err)
	}
	q, err := st.RetroQueue(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(q) != 3 || q[0].ID != a || q[1].ID != b || q[2].ID != c {
		t.Fatalf("queue order: %+v", q)
	}
	if q[0].Attempts != 1 || q[0].LastError != "RetroAchievements is down" || q[0].CreatedAt == 0 || q[0].UserAgent == "" {
		t.Fatalf("first row: %+v", q[0])
	}
	if n, _ := st.RetroQueueCount(ctx, "u_1"); n != 2 {
		t.Fatalf("u_1 has %d queued, want 2", n)
	}
	if err := st.DeleteRetroQueued(ctx, a); err != nil {
		t.Fatal(err)
	}
	if q, _ := st.RetroQueue(ctx, 10); len(q) != 2 || q[0].ID != b {
		t.Fatalf("after sending the first: %+v", q)
	}
}

// Turning earning off, disconnecting, and deleting the LANcast account each
// drop what was waiting: nothing is ever sent after any of them.
func TestNothingQueuedOutlivesTurningEarningOff(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, "u_3", "three", "x", RoleMember); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"u_1", "u_2", "u_3"} {
		connectRetro(t, st, u, "01"+u)
		if err := st.SetRetroConnectToken(ctx, u, []byte{9}); err != nil {
			t.Fatal(err)
		}
		queueOne(t, st, u, "awardachievement")
	}
	keep := queueOne(t, st, "u_keep", "awardachievement")

	if err := st.SetRetroConnectToken(ctx, "u_1", nil); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteRetroAccount(ctx, "u_2"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(ctx, "u_3"); err != nil {
		t.Fatal(err)
	}
	q, _ := st.RetroQueue(ctx, 10)
	if len(q) != 1 || q[0].ID != keep {
		t.Fatalf("left in the queue: %+v; want only the untouched account's", q)
	}
}
