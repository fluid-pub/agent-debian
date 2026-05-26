package exec

import "testing"

func TestTailString(t *testing.T) {
	s := "abcdefghijklmnopqrstuvwxyz"
	out := tailString(s, 5)
	if out != "vwxyz" {
		t.Fatalf("unexpected tail: %q", out)
	}
}
