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
