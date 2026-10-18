package errors

import (
	"testing"
	"time"
)

// within fails t if f doesn't return within 2s; a self-unwrapping error
// makes Is/As loop forever.
func within(t *testing.T, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("did not terminate")
	}
}

func TestEntityErrorUnwrap(t *testing.T) {
	sentinel := New("sentinel")

	t.Run("Is", func(t *testing.T) {
		within(t, func() {
			if !Is(Ent("x", sentinel), sentinel) {
				t.Error("Is(Ent(x, sentinel), sentinel) = false")
			}
		})
	})
	t.Run("As", func(t *testing.T) {
		within(t, func() {
			var ee EntityError
			if !As(Wrap("ctx", Ent("x", sentinel)), &ee) || ee.EntityIdentifier() != "x" {
				t.Errorf("As = %v, want EntityError x", ee)
			}
		})
	})
	t.Run("EntMsg terminates", func(t *testing.T) {
		within(t, func() {
			if Is(EntMsg("x", "boom"), sentinel) {
				t.Error("Is(EntMsg, sentinel) = true")
			}
		})
	})
}
