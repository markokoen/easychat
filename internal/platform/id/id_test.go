package id

import (
	"errors"
	"testing"
)

func TestNewUsesRandomBytes(t *testing.T) {
	origRead := randRead
	origNow := nowUnixNano
	t.Cleanup(func() {
		randRead = origRead
		nowUnixNano = origNow
	})

	randRead = func(b []byte) (int, error) {
		for i := range b {
			b[i] = byte(i + 1)
		}
		return len(b), nil
	}

	id := New()
	if id != "0102030405060708090a0b0c" {
		t.Fatalf("unexpected id: %s", id)
	}
}

func TestNewFallsBackOnRandomError(t *testing.T) {
	origRead := randRead
	origNow := nowUnixNano
	t.Cleanup(func() {
		randRead = origRead
		nowUnixNano = origNow
	})

	randRead = func(_ []byte) (int, error) {
		return 0, errors.New("boom")
	}
	nowUnixNano = func() int64 { return 12345 }

	if got := New(); got != "12345" {
		t.Fatalf("expected fallback timestamp, got %s", got)
	}
}
