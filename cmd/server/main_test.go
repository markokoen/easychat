package main

import "testing"

func TestMainRunServerSuccess(t *testing.T) {
	origRun := runServer
	origExit := exitProcess
	t.Cleanup(func() {
		runServer = origRun
		exitProcess = origExit
	})

	runServer = func() error { return nil }
	exitCalled := false
	exitProcess = func(int) {
		exitCalled = true
	}

	main()
	if exitCalled {
		t.Fatalf("did not expect exit on success")
	}
}

func TestMainRunServerFailure(t *testing.T) {
	origRun := runServer
	origExit := exitProcess
	t.Cleanup(func() {
		runServer = origRun
		exitProcess = origExit
	})

	runServer = func() error { return assertErr{} }
	exitCode := 0
	exitProcess = func(code int) {
		exitCode = code
	}

	main()
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

type assertErr struct{}

func (assertErr) Error() string { return "boom" }
