package mpv

import (
	"math"
	"strings"
	"testing"
)

// Every control at every channel count, spelled out. The graph is what mpv
// runs, so the assertion is the whole string rather than a substring: a
// changed constant should have to change a line here too.
func TestAudioFilter(t *testing.T) {
	const (
		night  = "acompressor=threshold=0.063:ratio=4:attack=10:release=250:makeup=4,alimiter=limit=0.9:level=0"
		low51  = "aeval=exprs=val(0)*0.5|val(1)*0.5|val(2)|val(3)*0.5|val(4)*0.5|val(5)*0.5:c=same"
		high51 = "aeval=exprs=val(0)*0.35|val(1)*0.35|val(2)|val(3)*0.35|val(4)*0.35|val(5)*0.35:c=same"
		high71 = "aeval=exprs=val(0)*0.35|val(1)*0.35|val(2)|val(3)*0.35|val(4)*0.35|val(5)*0.35|val(6)*0.35|val(7)*0.35:c=same"
	)
	cases := []struct {
		name     string
		fx       AudioFX
		channels int
		want     string
	}{
		{"everything off is no filter at all", AudioFX{}, 6, ""},
		{"night on stereo", AudioFX{Night: true}, 2, "lavfi=[" + night + "]"},
		{"night on 5.1", AudioFX{Night: true}, 6, "lavfi=[" + night + "]"},
		{"night before mpv reports channels", AudioFX{Night: true}, 0, "lavfi=[" + night + "]"},
		{"dialogue low on 5.1", AudioFX{Dialogue: 1}, 6, "lavfi=[" + low51 + "]"},
		{"dialogue high on 5.1", AudioFX{Dialogue: 2}, 6, "lavfi=[" + high51 + "]"},
		{"dialogue high on 7.1", AudioFX{Dialogue: 2}, 8, "lavfi=[" + high71 + "]"},
		{"dialogue low on stereo", AudioFX{Dialogue: 1}, 2, "lavfi=[dialoguenhance=enhance=1]"},
		{"dialogue high on stereo", AudioFX{Dialogue: 2}, 2, "lavfi=[dialoguenhance=enhance=2]"},
		{"boost before night, so night levels the boosted balance", AudioFX{Night: true, Dialogue: 2}, 6, "lavfi=[" + high51 + "," + night + "]"},
		// Mono has no dialogue to separate; 3 to 5 channels are layouts where
		// index 2 is not reliably the centre (quad, 4.0 vs 2.1, 5.0 vs 4.1).
		{"dialogue on mono is nothing", AudioFX{Dialogue: 2}, 1, ""},
		{"dialogue on quad is nothing", AudioFX{Dialogue: 2}, 4, ""},
		{"dialogue waits for a channel count", AudioFX{Dialogue: 2}, 0, ""},
		{"dialogue on mono keeps night", AudioFX{Night: true, Dialogue: 2}, 1, "lavfi=[" + night + "]"},
		// Clamped, not trusted: the level arrives from the page.
		{"a level above the top is the top", AudioFX{Dialogue: 99}, 6, "lavfi=[" + high51 + "]"},
		{"a negative level is off", AudioFX{Dialogue: -3}, 6, ""},
	}
	for _, c := range cases {
		if got := AudioFilter(c.fx, c.channels); got != c.want {
			t.Errorf("%s: AudioFilter(%+v, %d)\n got %q\nwant %q", c.name, c.fx, c.channels, got, c.want)
		}
	}
}

// Nothing the caller controls can reach the graph as text. The only inputs are
// a bool and two ints, and this checks that no value of them yields a filter
// outside the fixed vocabulary — in particular `amovie`/`movie`, the lavfi
// sources that open a file by path, which is why the page never sends a graph.
func TestAudioFilterVocabularyIsClosed(t *testing.T) {
	allowed := map[string]bool{"acompressor": true, "alimiter": true, "aeval": true, "dialoguenhance": true}
	for _, night := range []bool{false, true} {
		for d := -2; d <= DialogueMax+2; d++ {
			for ch := -1; ch <= 16; ch++ {
				g := AudioFilter(AudioFX{Night: night, Dialogue: d}, ch)
				if g == "" {
					continue
				}
				if !strings.HasPrefix(g, "lavfi=[") || !strings.HasSuffix(g, "]") {
					t.Fatalf("%q is not a single bracketed lavfi graph", g)
				}
				for _, f := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(g, "lavfi=["), "]"), ",") {
					name, _, _ := strings.Cut(f, "=")
					if !allowed[name] {
						t.Errorf("night=%v dialogue=%d channels=%d produced filter %q", night, d, ch, name)
					}
				}
			}
		}
	}
}

func TestWithAppliesOnlyAudioControls(t *testing.T) {
	inf := math.Inf(1)
	cases := []struct {
		name  string
		value float64
		want  AudioFX
		ok    bool
	}{
		{"night", 1, AudioFX{Night: true}, true},
		{"night", 0, AudioFX{}, true},
		{"dialogue", 1, AudioFX{Dialogue: 1}, true},
		{"dialogue", 1.9, AudioFX{Dialogue: 1}, true},
		{"dialogue", 7, AudioFX{Dialogue: DialogueMax}, true},
		{"dialogue", -1, AudioFX{}, true},
		{"dialogue", math.NaN(), AudioFX{}, true},
		{"dialogue", inf, AudioFX{Dialogue: DialogueMax}, true},
		{"dialogue", -inf, AudioFX{}, true},
		// Not an audio control: unchanged, and said so, so the caller's
		// closed set is still what refuses it.
		{"volume", 1, AudioFX{}, false},
		{"af", 1, AudioFX{}, false},
	}
	for _, c := range cases {
		got, ok := AudioFX{}.With(c.name, c.value)
		if got != c.want || ok != c.ok {
			t.Errorf("With(%q, %v) = %+v, %v; want %+v, %v", c.name, c.value, got, ok, c.want, c.ok)
		}
	}
}
