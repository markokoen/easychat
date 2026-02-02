package logger

import "testing"

func TestNew(t *testing.T) {
	log := New()
	if log == nil {
		t.Fatalf("expected logger instance")
	}
	log.Info("logger works")
}
