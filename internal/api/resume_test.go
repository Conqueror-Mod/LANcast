package api

import (
	"encoding/json"
	"testing"
	"time"
)

type resumeBody struct {
	Cards []struct {
		Kind      string `json:"kind"`
		TouchedAt int64  `json:"touched_at"`
		Item      struct {
			ID int64 `json:"id"`
		} `json:"item"`
	} `json:"cards"`
	More bool `json:"more"`
}

func getResume(t *testing.T, h *harness, path string) resumeBody {
	t.Helper()
	resp := h.do(t, "GET", path, nil)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s = %d", path, resp.StatusCode)
	}
	var b resumeBody
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

/*
 * The console shows at most two of a kind, newest first, and says when it left
 * something out; the See all page (?all=true) shows everything (ADR 0118 §2).
 *
 * Three films played a second apart, because a played time is whole seconds
 * and the order is the thing being asserted.
 */
func TestResumeCapsAKindAndOffersSeeAll(t *testing.T) {
	h := newHarness(t)
	var ids []int64
	for i, name := range []string{"a.mkv", "b.mkv", "c.mkv"} {
		if i > 0 {
			time.Sleep(1100 * time.Millisecond)
		}
		id := h.addFile(t, name, []byte(name))
		resp := h.do(t, "PUT", "/api/items/"+itoa(id)+"/progress",
			map[string]any{"position_ms": 1000, "watched": false})
		resp.Body.Close()
		ids = append(ids, id)
	}

	b := getResume(t, h, "/api/resume")
	if len(b.Cards) != 2 || !b.More {
		t.Fatalf("console = %d cards, more %v; want 2 and more — three films, two of a kind", len(b.Cards), b.More)
	}
	if b.Cards[0].Item.ID != ids[2] || b.Cards[1].Item.ID != ids[1] {
		t.Errorf("console order = %d, %d; want the newest first (%d, %d)",
			b.Cards[0].Item.ID, b.Cards[1].Item.ID, ids[2], ids[1])
	}
	for _, c := range b.Cards {
		if c.Kind != "video" || c.TouchedAt == 0 {
			t.Errorf("card = %+v, want kind video with a touched time", c)
		}
	}

	all := getResume(t, h, "/api/resume?all=true")
	if len(all.Cards) != 3 || all.More {
		t.Errorf("see all = %d cards, more %v; want all 3 and nothing more", len(all.Cards), all.More)
	}

	one := getResume(t, h, "/api/resume?limit=1")
	if len(one.Cards) != 1 || !one.More {
		t.Errorf("limit 1 = %d cards, more %v; want 1 and more", len(one.Cards), one.More)
	}
}
