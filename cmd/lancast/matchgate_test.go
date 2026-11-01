package main

import "testing"

func TestMatchGateOffPlaysAtOnce(t *testing.T) {
	var g matchGate
	g.open(false)
	if !g.play() {
		t.Fatal("with matching off, play must unpause at once")
	}
}

func TestMatchGateHoldsPlayUntilSettled(t *testing.T) {
	var g matchGate
	gen := g.open(true)
	if g.play() {
		t.Fatal("play went through while the display was being matched")
	}
	play, ok := g.settled(gen)
	if !ok || !play {
		t.Fatalf("settled = %v, %v; want the held play honoured", play, ok)
	}
	if !g.play() {
		t.Fatal("after settling, play must go straight through")
	}
}

func TestMatchGateSettledBeforePlay(t *testing.T) {
	// The display settled before the page asked: nothing to start yet, and
	// the page's later play goes straight through.
	var g matchGate
	gen := g.open(true)
	if play, ok := g.settled(gen); !ok || play {
		t.Fatalf("settled = %v, %v; want ok and no play", play, ok)
	}
	if !g.play() {
		t.Fatal("play after settling was held")
	}
}

func TestMatchGatePauseCancelsHeldPlay(t *testing.T) {
	var g matchGate
	gen := g.open(true)
	g.play()
	g.pause()
	if play, ok := g.settled(gen); !ok || play {
		t.Fatalf("settled = %v, %v; a paused film must not start", play, ok)
	}
}

func TestMatchGateStaleMatchChangesNothing(t *testing.T) {
	var g matchGate
	first := g.open(true)
	g.play()
	second := g.open(true) // the next film in a queue, before the first settled
	if _, ok := g.settled(first); ok {
		t.Fatal("the first film's match settled the second film")
	}
	if g.play() {
		t.Fatal("the second film's play went through before its own match")
	}
	if play, ok := g.settled(second); !ok || !play {
		t.Fatalf("second settled = %v, %v", play, ok)
	}
}

func TestMatchGateStopDuringSettle(t *testing.T) {
	var g matchGate
	gen := g.open(true)
	g.play()
	g.stop()
	if _, ok := g.settled(gen); ok {
		t.Fatal("a stopped film was started when its display settled")
	}
}
