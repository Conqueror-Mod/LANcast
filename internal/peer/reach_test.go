package peer

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

// hanging is an address that accepts a connection and then says nothing,
// which is how a dead VPN interface behaves from the far side: no refusal,
// just silence until somebody gives up.
func hanging(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range held {
			c.Close()
		}
		mu.Unlock()
	})
	return ln.Addr().String()
}

// refused is an address nothing listens on, which fails at once.
func refused(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func live(t *testing.T) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

var anyCert = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test servers only

// The fault this exists for: a dead address first, a live one second. The
// live one must come out first, and quickly — not after the dead one has had
// the whole budget.
func TestReachPutsTheLiveAddressFirst(t *testing.T) {
	dead, ok := hanging(t), live(t)
	start := time.Now()
	got := Reach(context.Background(), anyCert, []string{dead, ok}, 100*time.Millisecond)
	if want := []string{ok, dead}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	if el := time.Since(start); el > time.Second {
		t.Errorf("took %v; the dead address held it up", el)
	}
}

// A healthy first address keeps its place, and the order is otherwise kept.
func TestReachKeepsAWorkingOrder(t *testing.T) {
	a, b := live(t), hanging(t)
	if got := Reach(context.Background(), anyCert, []string{a, b}, 100*time.Millisecond); got[0] != a {
		t.Errorf("order = %v, want %s first", got, a)
	}
}

// Nothing answering is reported at once when every address refuses, and the
// list comes back as it went in for the caller to fail on as before.
func TestReachWithNothingAnswering(t *testing.T) {
	in := []string{refused(t), refused(t)}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if got := Reach(ctx, anyCert, in, 50*time.Millisecond); !reflect.DeepEqual(got, in) {
		t.Errorf("order = %v, want unchanged", got)
	}
	if el := time.Since(start); el > time.Second {
		t.Errorf("took %v to learn that everything refused", el)
	}

	// All silent: bounded by the context, and still unchanged.
	silent := []string{hanging(t), hanging(t)}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel2()
	if got := Reach(ctx2, anyCert, silent, 50*time.Millisecond); !reflect.DeepEqual(got, silent) {
		t.Errorf("order = %v, want unchanged", got)
	}
}
