package artwork

import (
	"testing"
)

// The embedding worker takes paths, so a cached original is handed over as
// one — and only when it is really there.
func TestOriginalPathOnlyForWhatIsStored(t *testing.T) {
	c := New(t.TempDir())
	hash, _, _, _, err := c.Put(makeJPEG(t, 40, 30, 128))
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := c.OriginalPath(hash); !ok || p == "" {
		t.Errorf("OriginalPath(%q) = %q, %v; want the stored file", hash, p, ok)
	}
	if _, ok := c.OriginalPath("0000000000000000000000000000000000000000000000000000000000000000"); ok {
		t.Error("a hash that was never stored has a path")
	}
	if _, ok := c.OriginalPath("../../etc"); ok {
		t.Error("a malformed hash has a path")
	}
}
