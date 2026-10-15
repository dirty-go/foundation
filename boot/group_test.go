package boot

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// fakeServer blocks in Serve until Shutdown, unless configured otherwise. It
// implements Server but not ReadyNotifier.
type fakeServer struct {
	name          string
	serveErr      error         // Serve returns it immediately
	returnNil     bool          // Serve returns nil immediately
	stopErr       error         // Serve returns it once stopped by Shutdown
	shutdownErr   error         // Shutdown returns it
	shutdownDelay time.Duration // Shutdown waits this long, or until ctx is done
	stuck         bool          // Serve ignores Shutdown until the test ends
	panicIn       string        // "serve", "shutdown" or "name"
	onShutdown    func()

	accepting atomic.Bool
	polls     atomic.Int32
	shutdowns atomic.Int32
	started   chan struct{}
	startOnce sync.Once
	stop      chan struct{}
	stopOnce  sync.Once
	release   chan struct{}
}

func newFake(t *testing.T, name string) *fakeServer {
	t.Helper()
	s := &fakeServer{
		name:    name,
		started: make(chan struct{}),
		stop:    make(chan struct{}),
		release: make(chan struct{}),
	}
	t.Cleanup(func() { close(s.release) })
	return s
}

func (s *fakeServer) ServerName() string {
	if s.panicIn == "name" {
		panic("name boom")
	}
	return s.name
}

func (s *fakeServer) Serve() error {
	s.startOnce.Do(func() { close(s.started) })
	switch {
	case s.panicIn == "serve":
		panic("serve boom")
	case s.serveErr != nil:
		return s.serveErr
	case s.returnNil:
		return nil
	case s.stuck:
		<-s.release
		return nil
	}
	<-s.stop
	return s.stopErr
}

func (s *fakeServer) Shutdown(ctx context.Context) error {
	s.shutdowns.Add(1)
	defer s.stopOnce.Do(func() { close(s.stop) })
	if s.onShutdown != nil {
		s.onShutdown()
	}
	if s.panicIn == "shutdown" {
		panic(errors.New("shutdown boom"))
	}
	if s.shutdownDelay > 0 {
		select {
		case <-time.After(s.shutdownDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.shutdownErr
}

func (s *fakeServer) IsAcceptingClients() bool {
	s.polls.Add(1)
	return s.accepting.Load()
}

func (s *fakeServer) IsHealthy() bool { return s.accepting.Load() }

func testGroup(opts ...Option) *Group {
	return NewGroup(append([]Option{WithLogger(zerolog.Nop())}, opts...)...)
}

func mustGroup(t *testing.T, servers ...Server) *Group {
	t.Helper()
	g := testGroup()
	if err := g.Add(servers...); err != nil {
		t.Fatal(err)
	}
	return g
}

// runAsync runs g in a goroutine; the returned channel yields Run's result.
func runAsync(ctx context.Context, g *Group) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- g.Run(ctx) }()
	return ch
}

func waitErr(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

// eventually fails t unless cond becomes true within 2s.
func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		runtime.Gosched()
	}
}

// never fails t if cond becomes true within d.
func never(t *testing.T, cond func() bool, d time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			t.Fatal(msg)
		}
		runtime.Gosched()
	}
}

func TestRunServeErrorShutsDownAll(t *testing.T) {
	errBoom := errors.New("boom")
	failing := newFake(t, "failing")
	failing.serveErr = errBoom
	other := newFake(t, "other")

	g := mustGroup(t, failing, other)
	err := waitErr(t, runAsync(t.Context(), g))
	if !errors.Is(err, errBoom) {
		t.Fatalf("Run = %v, want %v", err, errBoom)
	}
	if !strings.Contains(err.Error(), "failing serve") {
		t.Errorf("Run = %q, want server name in error", err)
	}
	for _, s := range []*fakeServer{failing, other} {
		if s.shutdowns.Load() != 1 {
			t.Errorf("%s: Shutdown called %d times, want 1", s.name, s.shutdowns.Load())
		}
	}
	waitClosed(t, g.Done(), true)
	if !errors.Is(g.Err(), errBoom) {
		t.Errorf("Err = %v, want %v", g.Err(), errBoom)
	}
	waitClosed(t, g.Ready(), false)
}

func TestRunCtxCancel(t *testing.T) {
	a, b := newFake(t, "a"), newFake(t, "b")
	ctx, cancel := context.WithCancel(t.Context())
	ch := make(chan error, 1)
	go func() { ch <- Run(ctx, a, b) }()

	waitClosed(t, a.started, true)
	waitClosed(t, b.started, true)
	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if a.shutdowns.Load() != 1 || b.shutdowns.Load() != 1 {
		t.Errorf("Shutdown calls = %d, %d; want 1, 1", a.shutdowns.Load(), b.shutdowns.Load())
	}
}

func TestRunShutdownTimeout(t *testing.T) {
	slow := newFake(t, "slow")
	slow.shutdownDelay = time.Minute

	g := testGroup(WithShutdownTimeout(50 * time.Millisecond))
	if err := g.Add(slow); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	cancel()

	start := time.Now()
	err := waitErr(t, ch)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run = %v, want DeadlineExceeded", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("Run took %v after cancel, want ~50ms", d)
	}
}

func TestRunServeAndShutdownErrors(t *testing.T) {
	errServe, errShutdown := errors.New("serve failed"), errors.New("shutdown failed")
	failing := newFake(t, "failing")
	failing.serveErr = errServe
	other := newFake(t, "other")
	other.shutdownErr = errShutdown

	err := waitErr(t, runAsync(t.Context(), mustGroup(t, failing, other)))
	if !errors.Is(err, errServe) || !errors.Is(err, errShutdown) {
		t.Fatalf("Run = %v, want both %v and %v", err, errServe, errShutdown)
	}
}

func TestRunServeErrorAfterCancel(t *testing.T) {
	errStop := errors.New("unclean stop")
	s := newFake(t, "s")
	s.stopErr = errStop

	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, mustGroup(t, s))
	waitClosed(t, s.started, true)
	cancel()
	if err := waitErr(t, ch); !errors.Is(err, errStop) {
		t.Fatalf("Run = %v, want %v", err, errStop)
	}
}

func TestRunEarlyNilKeepsOthersRunning(t *testing.T) {
	early := newFake(t, "early")
	early.returnNil = true
	other := newFake(t, "other")

	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, mustGroup(t, early, other))
	waitClosed(t, early.started, true)
	never(t, func() bool { return len(ch) > 0 || other.shutdowns.Load() > 0 },
		100*time.Millisecond, "Run stopped after an early nil Serve")

	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if other.shutdowns.Load() != 1 {
		t.Error("other server not shut down after cancel")
	}
}

func TestRunAllNilReturns(t *testing.T) {
	a, b := newFake(t, "a"), newFake(t, "b")
	a.returnNil, b.returnNil = true, true

	g := mustGroup(t, a, b)
	if err := waitErr(t, runAsync(t.Context(), g)); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if a.shutdowns.Load() != 0 || b.shutdowns.Load() != 0 {
		t.Error("Shutdown called on servers that already stopped")
	}
	// Deterministic: Ready is closed when every Serve returned nil.
	waitClosed(t, g.Ready(), true)
}

func TestRunNilServer(t *testing.T) {
	for name, srv := range map[string]Server{
		"nil":            nil,
		"typed nil HTTP": (*HTTP)(nil),
		"typed nil fake": (*fakeServer)(nil),
	} {
		t.Run(name, func(t *testing.T) {
			if err := Run(t.Context(), newFake(t, "ok"), srv); !errors.Is(err, ErrNilServer) {
				t.Fatalf("Run = %v, want ErrNilServer", err)
			}
		})
	}
}

func TestRunPanicServe(t *testing.T) {
	p := newFake(t, "panicky")
	p.panicIn = "serve"
	other := newFake(t, "other")

	err := waitErr(t, runAsync(t.Context(), mustGroup(t, p, other)))
	if err == nil || !strings.Contains(err.Error(), "panicky serve: panic: string") {
		t.Fatalf("Run = %v, want recovered serve panic", err)
	}
	if strings.Contains(err.Error(), "serve boom") {
		t.Errorf("Run = %v, must not include the panic value", err)
	}
	if other.shutdowns.Load() != 1 {
		t.Error("other server not shut down after panic")
	}
}

func TestRunPanicShutdown(t *testing.T) {
	p := newFake(t, "panicky")
	p.panicIn = "shutdown"

	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, mustGroup(t, p))
	waitClosed(t, p.started, true)
	cancel()
	err := waitErr(t, ch)
	if err == nil || !strings.Contains(err.Error(), "panicky shutdown: panic: shutdown boom") {
		t.Fatalf("Run = %v, want recovered shutdown panic", err)
	}
}

func TestRunPanicServerName(t *testing.T) {
	p := newFake(t, "panicky")
	p.panicIn = "name"
	other := newFake(t, "other")

	g := mustGroup(t, p, other)
	err := waitErr(t, runAsync(t.Context(), g))
	if err == nil || !strings.Contains(err.Error(), "ServerName: panic: string") {
		t.Fatalf("Run = %v, want recovered ServerName panic", err)
	}
	waitClosed(t, other.started, false)
	waitClosed(t, g.Ready(), false)
}

func TestRunHardDeadline(t *testing.T) {
	stuck := newFake(t, "stuck")
	stuck.stuck = true
	fine := newFake(t, "fine")

	g := testGroup(WithShutdownTimeout(20*time.Millisecond), WithShutdownGrace(20*time.Millisecond))
	if err := g.Add(stuck, fine); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	waitClosed(t, stuck.started, true)
	cancel()

	err := waitErr(t, ch)
	if !errors.Is(err, ErrAbandoned) {
		t.Fatalf("Run = %v, want ErrAbandoned", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "serve: stuck") || strings.Contains(msg, "fine") {
		t.Errorf("Run = %q, want only the stuck server named", msg)
	}
	waitClosed(t, g.Done(), true)
}

func TestRunErrStartedNoLeak(t *testing.T) {
	g := testGroup()
	if err := g.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, warm := SignalContext(t.Context()) // starts os/signal's own goroutine once
	warm()

	base := runtime.NumGoroutine()
	ctx, stop := SignalContext(t.Context())
	if err := g.Run(ctx); !errors.Is(err, ErrStarted) {
		t.Fatalf("second Run = %v, want ErrStarted", err)
	}
	stop()
	eventually(t, func() bool { return runtime.NumGoroutine() <= base }, "goroutine leaked after ErrStarted")
}

func TestGroupAddAfterRun(t *testing.T) {
	g := mustGroup(t, newFake(t, "a"))
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	eventually(t, func() bool {
		g.mu.Lock()
		defer g.mu.Unlock()
		return g.started
	}, "Run did not start")

	if err := g.Add(newFake(t, "b")); !errors.Is(err, ErrStarted) {
		t.Errorf("Add after Run = %v, want ErrStarted", err)
	}
	if err := g.Run(ctx); !errors.Is(err, ErrStarted) {
		t.Errorf("second Run = %v, want ErrStarted", err)
	}
	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if err := g.Run(t.Context()); !errors.Is(err, ErrStarted) {
		t.Errorf("Run after Run = %v, want ErrStarted", err)
	}
}

func TestGroupAddNil(t *testing.T) {
	g := testGroup()
	for _, srv := range []Server{nil, (*HTTP)(nil)} {
		if err := g.Add(newFake(t, "a"), srv); !errors.Is(err, ErrNilServer) {
			t.Fatalf("Add(%#v) = %v, want ErrNilServer", srv, err)
		}
	}
	if len(g.servers) != 0 {
		t.Error("Add(nil) added servers")
	}
}

func TestGroupAllAcceptingConcurrentAdd(t *testing.T) {
	g := testGroup()
	var wg sync.WaitGroup
	wg.Add(8)
	for range 4 {
		go func() {
			defer wg.Done()
			for range 50 {
				s := newFake(t, "s")
				s.accepting.Store(true)
				if err := g.Add(s); err != nil {
					t.Error(err)
				}
			}
		}()
		go func() {
			defer wg.Done()
			for range 50 {
				if !g.AllAccepting() {
					t.Error("AllAccepting = false with accepting servers")
				}
			}
		}()
	}
	wg.Wait()
}

func TestGroupReady(t *testing.T) {
	notifier := newTestHTTP(t)
	poller := newFake(t, "poll")

	g := mustGroup(t, notifier, poller)
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)

	waitClosed(t, notifier.Ready(), true)
	waitClosed(t, g.Ready(), false) // poller not ready yet
	if g.AllAccepting() {
		t.Error("AllAccepting = true with poller not accepting")
	}

	poller.accepting.Store(true)
	waitClosed(t, g.Ready(), true)
	if !g.AllAccepting() {
		t.Error("AllAccepting = false after Ready")
	}

	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestGroupReadyNotClosedAfterShutdownBegins(t *testing.T) {
	poller := newFake(t, "poll")
	poller.onShutdown = func() { poller.accepting.Store(true) }

	g := mustGroup(t, poller)
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	waitClosed(t, poller.started, true)
	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
	waitClosed(t, g.Ready(), false)
}

func TestGroupReadyWithoutServers(t *testing.T) {
	var g Group // zero value
	ch := g.Ready()
	waitClosed(t, ch, false)
	if err := g.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, ch, true)
	if g.Ready() != ch {
		t.Error("Ready returned a different channel")
	}
	if !g.AllAccepting() {
		t.Error("AllAccepting = false for empty group")
	}
}

func TestGroupStartupFailureReportedByDone(t *testing.T) {
	notifier := NewHTTP("bad", &http.Server{Addr: "invalid-addr", ReadHeaderTimeout: time.Second})
	g := mustGroup(t, notifier, newFake(t, "other"))
	runAsync(t.Context(), g)

	select {
	case <-g.Ready():
		t.Fatal("Ready closed despite startup failure")
	case <-g.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("neither Ready nor Done closed")
	}
	if err := g.Err(); err == nil || !strings.Contains(err.Error(), "bad serve: listen invalid-addr") {
		t.Errorf("Err = %v, want name and address", err)
	}
}

func TestGroupErrBeforeDone(t *testing.T) {
	g := testGroup()
	if err := g.Err(); err != nil {
		t.Errorf("Err before Run = %v", err)
	}
}

func TestGroupDoneThenErr(t *testing.T) {
	errBoom := errors.New("boom")
	failing := newFake(t, "failing")
	failing.serveErr = errBoom
	g := mustGroup(t, failing)

	seen := make(chan error, 1)
	go func() {
		<-g.Done()
		seen <- g.Err()
	}()
	runErr := g.Run(t.Context())
	if got := <-seen; got != runErr || !errors.Is(got, errBoom) { //nolint:errorlint // identity check
		t.Fatalf("Err after Done = %v, want Run's result %v", got, runErr)
	}
}

func TestGroupReadyWaiterStopsAfterRun(t *testing.T) {
	poller := newFake(t, "never-ready")
	g := mustGroup(t, poller)
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)

	eventually(t, func() bool { return poller.polls.Load() > 1 }, "readiness waiter never polled")
	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}

	polls := poller.polls.Load()
	never(t, func() bool { return poller.polls.Load() != polls },
		3*readyPollInterval, "waiter still polling after Run returned")
	waitClosed(t, g.Ready(), false)
}

var _ Server = (*fakeServer)(nil)
