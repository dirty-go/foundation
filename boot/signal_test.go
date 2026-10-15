//go:build unix

package boot

import (
	"context"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// stubExit replaces exit for the test and returns the codes it receives.
func stubExit(t *testing.T) <-chan int {
	t.Helper()
	codes := make(chan int, 1)
	exit = func(code int) { codes <- code }
	t.Cleanup(func() { exit = osExit })
	return codes
}

var osExit = exit

func sigterm(t *testing.T) {
	t.Helper()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
}

func TestSignalContextGracefulStop(t *testing.T) {
	stubExit(t)
	s := newFake(t, "s")
	ctx, stop := SignalContext(t.Context())
	defer stop()

	ch := runAsync(ctx, mustGroup(t, s))
	waitClosed(t, s.started, true)
	sigterm(t)
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if s.shutdowns.Load() != 1 {
		t.Error("server not shut down")
	}
	if cause := context.Cause(ctx); cause == nil || !strings.Contains(cause.Error(), "terminated") {
		t.Errorf("Cause = %v, want the signal", cause)
	}
}

func TestSignalContextSecondSignalExits(t *testing.T) {
	codes := stubExit(t)
	ctx, stop := SignalContext(t.Context())
	defer stop()

	sigterm(t)
	waitClosed(t, ctx.Done(), true)
	sigterm(t)
	select {
	case code := <-codes:
		if code != 143 {
			t.Errorf("exit code = %d, want 143", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second signal did not exit")
	}
}

func TestSignalContextStopNoLeak(t *testing.T) {
	_, warm := SignalContext(t.Context()) // starts os/signal's own goroutine once
	warm()

	base := runtime.NumGoroutine()
	ctx, stop := SignalContext(t.Context())
	stop()
	stop() // idempotent
	if ctx.Err() == nil {
		t.Error("ctx not cancelled by stop")
	}
	eventually(t, func() bool { return runtime.NumGoroutine() <= base }, "SignalContext leaked a goroutine")
}

func TestRunSignals(t *testing.T) {
	stubExit(t)
	s := newFake(t, "s")
	ch := make(chan error, 1)
	go func() { ch <- RunSignals(s) }()

	waitClosed(t, s.started, true)
	sigterm(t)
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("RunSignals = %v, want nil", err)
	}
}
