package main

import (
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestExtraPortIsNilWhenTheMainPortIs443(t *testing.T) {
	if newExtraPort(":443", func(net.Listener) {}, slog.New(slog.NewTextHandler(io.Discard, nil))) != nil {
		t.Fatal("a server already on 443 must not try to open it twice")
	}
	if p := newExtraPort("0.0.0.0:8080", func(net.Listener) {}, nil); p == nil || p.addr != "0.0.0.0:443" {
		t.Fatalf("got %+v; the extra port belongs on the main listener's host", p)
	}
}

// Opening serves on the port; closing releases it so it can be opened again.
// A real port is used, chosen free, because what is being proven is the bind.
func TestExtraPortOpensAndCloses(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	probe.Close()

	served := make(chan net.Listener, 4)
	p := &extraPort{addr: addr, log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		serve: func(ln net.Listener) {
			served <- ln
			for {
				c, err := ln.Accept()
				if err != nil {
					if !closedListener(err) {
						t.Errorf("accept: %v", err)
					}
					return
				}
				c.Close()
			}
		}}

	if err := p.Set(true); err != nil {
		t.Fatal(err)
	}
	if err := p.Set(true); err != nil {
		t.Fatalf("opening an open port: %v", err)
	}
	select {
	case <-served:
	case <-time.After(2 * time.Second):
		t.Fatal("the port was opened but never served")
	}
	if c, err := net.Dial("tcp", addr); err != nil {
		t.Fatalf("nothing answering on the open port: %v", err)
	} else {
		c.Close()
	}

	if err := p.Set(false); err != nil {
		t.Fatal(err)
	}
	if c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("still answering after the port was closed")
	}
	// Released, so turning the certificate back on can open it again.
	if err := p.Set(true); err != nil {
		t.Fatalf("could not reopen: %v", err)
	}
	p.Close()
}

// A port somebody else holds is an error to report, not a crash.
func TestExtraPortReportsAPortInUse(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	p := &extraPort{addr: held.Addr().String(), serve: func(net.Listener) {},
		log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := p.Set(true); err == nil {
		t.Fatal("opening a port already in use reported success")
	}
	if p.ln != nil {
		t.Fatal("a failed open left a listener recorded")
	}
}
