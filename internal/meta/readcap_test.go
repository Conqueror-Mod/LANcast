package meta

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadCapped(t *testing.T) {
	if b, err := ReadCapped(strings.NewReader("12345"), 5); err != nil || string(b) != "12345" {
		t.Errorf("at the limit: %q, %v", b, err)
	}
	if _, err := ReadCapped(bytes.NewReader(make([]byte, 6)), 5); err == nil {
		t.Error("one byte over the limit was accepted")
	}
}
