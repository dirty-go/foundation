package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// fakeServer blocks in Serve until Shutdown, unless configured otherwise. It
// implements Server, AcceptingReporter and HealthReporter, but not
// ReadyNotifier. Wrap it in bare to hide the optional interfaces.
type fakeServer struct {
	name          string
	serveErr      error         // Serve returns it immediately
	returnNil     bool          // Serve returns nil immediately
	stopErr       error         // Serve returns it once stopped by Shutdown
	shutdownErr   error         // Shutdown returns it
	shutdownDelay time.Duration // Shutdown waits this long, or until ctx is done
	stuck         bool          // Serve ignores Shutdown until the test ends
	panicIn       string        // "serve", "shutdown" or "name"
	goexit        bool          // Serve calls runtime.Goexit
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
	case s.goexit:
		runtime.Goexit()
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

// bare exposes only the Server methods of the embedded server, hiding any
// optional interfaces.
type bare struct{ Server }

// stopServe makes a running Serve return nil without calling Shutdown.
func (s *fakeServer) stopServe() { s.stopOnce.Do(func() { close(s.stop) }) }

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
	var se *ShutdownError
	if !errors.As(err, &se) || se.Server != "other" {
		t.Errorf("Run = %v, want *ShutdownError for other", err)
	}
}

func TestRunShutdownErrorType(t *testing.T) {
	errShutdown := errors.New("shutdown failed")
	s := newFake(t, "s")
	s.shutdownErr = errShutdown

	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, mustGroup(t, s))
	waitClosed(t, s.started, true)
	cancel()
	err := waitErr(t, ch)
	var se *ShutdownError
	if !errors.As(err, &se) || se.Server != "s" || !errors.Is(se, errShutdown) {
		t.Fatalf("Run = %v, want *ShutdownError for s wrapping %v", err, errShutdown)
	}
	if got, want := err.Error(), "s shutdown: shutdown failed"; got != want {
		t.Errorf("Run = %q, want %q", got, want)
	}
}

func TestShutdownErrorNilErr(t *testing.T) {
	e := &ShutdownError{Server: "s"}
	if got, want := e.Error(), "s shutdown failed"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if e.Unwrap() != nil {
		t.Errorf("Unwrap() = %v, want nil", e.Unwrap())
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
	waitClosed(t, g.Done(), true)
	// Nothing is serving, so Ready stays open; consumers select on Done.
	waitClosed(t, g.Ready(), false)
	if g.AllAccepting() {
		t.Error("AllAccepting = true with every Serve returned")
	}
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
	if se := (*ShutdownError)(nil); !errors.As(err, &se) {
		t.Errorf("Run = %v, want a *ShutdownError", err)
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
	// Pin the documented shape: a joined leaf wrapping ErrAbandoned directly.
	if !hasAbandonedLeaf(err) {
		t.Errorf("Run = %v, want a joined leaf whose Unwrap is ErrAbandoned", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "serve: stuck") || strings.Contains(msg, "fine") {
		t.Errorf("Run = %q, want only the stuck server named", msg)
	}
	waitClosed(t, g.Done(), true)
}

// hasAbandonedLeaf reports whether err is, or joins, a leaf that directly
// wraps ErrAbandoned.
func hasAbandonedLeaf(err error) bool {
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		return slices.ContainsFunc(j.Unwrap(), hasAbandonedLeaf)
	}
	return errors.Unwrap(err) == ErrAbandoned //nolint:errorlint // structural shape check
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

func TestRunRepeatedWhileRunning(t *testing.T) {
	var buf syncBuffer
	s := newFake(t, "a")
	g := NewGroup(WithLogger(zerolog.New(&buf)))
	if err := g.Add(s); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	waitClosed(t, s.started, true)
	eventually(t, func() bool { return servingAt(g, 0) }, "Serve not running")
	logged := buf.Len()

	if err := g.Run(ctx); !errors.Is(err, ErrStarted) {
		t.Fatalf("second Run = %v, want ErrStarted", err)
	}
	if buf.Len() != logged {
		t.Errorf("second Run logged: %q", buf.String()[logged:])
	}
	if s.shutdowns.Load() != 0 || !servingAt(g, 0) || len(ch) > 0 {
		t.Error("second Run affected the running Group")
	}

	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
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
				g.AllAccepting() // true only while still empty; checked for races
			}
		}()
	}
	wg.Wait()
	if g.AllAccepting() {
		t.Error("AllAccepting before Run = true, want false: nothing is serving")
	}
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

func TestGroupReadyAcceptingOnly(t *testing.T) {
	poller := newFake(t, "poll")
	g := mustGroup(t, poller)
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)

	eventually(t, func() bool { return poller.polls.Load() > 1 }, "readiness waiter never polled")
	waitClosed(t, g.Ready(), false)
	poller.accepting.Store(true)
	waitClosed(t, g.Ready(), true)

	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestGroupReadyBare(t *testing.T) {
	g := mustGroup(t, bare{newFake(t, "bare")})
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)

	waitClosed(t, g.Ready(), true)

	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestGroupReadyMixed(t *testing.T) {
	notifier := newTestHTTP(t)
	poller := newFake(t, "poll")
	b := newFake(t, "bare")

	g := mustGroup(t, notifier, poller, bare{b})
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)

	waitClosed(t, notifier.Ready(), true)
	waitClosed(t, b.started, true)
	waitClosed(t, g.Ready(), false) // poller not ready yet
	if g.AllAccepting() {
		t.Error("AllAccepting = true with poller not accepting")
	}

	poller.accepting.Store(true)
	waitClosed(t, g.Ready(), true)
	if !g.AllAccepting() {
		t.Error("AllAccepting = false with every server accepting")
	}

	b.stopServe() // degraded: bare server no longer serving
	eventually(t, func() bool { return !g.AllAccepting() }, "AllAccepting still true after bare Serve returned")

	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestGroupAllAcceptingWithInterface(t *testing.T) {
	s := newFake(t, "s")
	s.accepting.Store(true)  // reports true throughout, even when not serving
	o := newFake(t, "other") // keeps the Group running once s stops
	g := mustGroup(t, s, bare{o})
	if g.AllAccepting() {
		t.Fatal("AllAccepting before Run = true, want false: Serve not running")
	}
	if n := s.polls.Load(); n != 0 {
		t.Fatalf("IsAcceptingClients called %d times before Run, want 0", n)
	}

	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	waitClosed(t, s.started, true)
	waitClosed(t, o.started, true)
	eventually(t, g.AllAccepting, "AllAccepting = false while serving and reporting true")
	s.accepting.Store(false)
	if g.AllAccepting() {
		t.Error("AllAccepting = true while serving and reporting false")
	}
	s.accepting.Store(true)

	s.stopServe()
	eventually(t, func() bool { return !servingAt(g, 0) }, "Serve did not return")
	if g.stopping.Load() {
		t.Fatal("Group stopping after one nil Serve")
	}
	polls := s.polls.Load()
	if g.AllAccepting() {
		t.Error("AllAccepting = true after Serve returned, want false despite the reporter's true")
	}
	if s.polls.Load() != polls {
		t.Error("AllAccepting asked IsAcceptingClients after Serve returned")
	}

	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestGroupAllAcceptingBare(t *testing.T) {
	s := newFake(t, "bare")
	g := mustGroup(t, bare{s})
	if g.AllAccepting() {
		t.Fatal("AllAccepting before Run = true, want false")
	}

	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	waitClosed(t, s.started, true)
	eventually(t, g.AllAccepting, "AllAccepting = false while serving")

	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if g.AllAccepting() {
		t.Error("AllAccepting after Serve returned = true, want false")
	}
}

func TestGroupAllAcceptingBareConcurrentRun(t *testing.T) {
	g := mustGroup(t, bare{newFake(t, "bare")})
	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 1000 {
			g.AllAccepting()
		}
	}()
	ch := runAsync(ctx, g)
	wg.Wait()
	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

// servingAt reports whether g's Serve for server i is running.
func servingAt(g *Group, i int) bool {
	g.mu.Lock()
	serving := g.serving
	g.mu.Unlock()
	return serving != nil && serving[i].Load()
}

// notifier adds a test-controlled ReadyNotifier to a Server.
type notifier struct {
	Server
	ch chan struct{}
}

func (n notifier) Ready() <-chan struct{} { return n.ch }

// waitReadyAsync runs waitReady in a goroutine; the channel yields its result.
func waitReadyAsync(stop <-chan struct{}, servers []Server, serving []atomic.Bool) <-chan bool {
	ch := make(chan bool, 1)
	go func() { ch <- waitReady(stop, servers, serving) }()
	return ch
}

func TestWaitReadyBareServeReturned(t *testing.T) {
	// A bare server whose Serve already returned (e.g. failed at once)
	// blocks readiness until stop.
	serving := make([]atomic.Bool, 1)
	stop := make(chan struct{})
	ch := waitReadyAsync(stop, []Server{bare{newFake(t, "failed")}}, serving)

	select {
	case got := <-ch:
		t.Fatalf("waitReady = %v before stop, want it to block", got)
	case <-time.After(100 * time.Millisecond):
	}
	close(stop)
	if <-ch {
		t.Error("waitReady = true, want false")
	}
}

func TestWaitReadyBareStoppedWhileWaiting(t *testing.T) {
	// A bare server that stops while waitReady blocks on a later server is
	// re-checked before reporting ready.
	n := notifier{bare{newFake(t, "slow")}, make(chan struct{})}
	serving := make([]atomic.Bool, 2)
	serving[0].Store(true)
	serving[1].Store(true)
	stop := make(chan struct{})
	ch := waitReadyAsync(stop, []Server{bare{newFake(t, "early")}, n}, serving)

	serving[0].Store(false)
	close(n.ch)
	select {
	case got := <-ch:
		t.Fatalf("waitReady = %v before stop, want it to block", got)
	case <-time.After(100 * time.Millisecond):
	}
	close(stop)
	if <-ch {
		t.Error("waitReady = true, want false")
	}
}

func TestWaitReadySignalledStoppedWhileWaiting(t *testing.T) {
	// A ReadyNotifier server that became ready and then stopped while
	// waitReady blocks on a later server is re-checked too.
	early := notifier{bare{newFake(t, "early")}, make(chan struct{})}
	slow := notifier{bare{newFake(t, "slow")}, make(chan struct{})}
	serving := make([]atomic.Bool, 2)
	serving[0].Store(true)
	serving[1].Store(true)
	stop := make(chan struct{})
	ch := waitReadyAsync(stop, []Server{early, slow}, serving)

	close(early.ch)
	serving[0].Store(false)
	close(slow.ch)
	select {
	case got := <-ch:
		t.Fatalf("waitReady = %v before stop, want it to block", got)
	case <-time.After(100 * time.Millisecond):
	}
	close(stop)
	if <-ch {
		t.Error("waitReady = true, want false")
	}
}

func TestWaitReadyBareServing(t *testing.T) {
	serving := make([]atomic.Bool, 1)
	serving[0].Store(true)
	if !waitReady(make(chan struct{}), []Server{bare{newFake(t, "ok")}}, serving) {
		t.Error("waitReady = false with a serving bare server")
	}
}

func TestGroupReadyBareStoppedWhileWaiting(t *testing.T) {
	early := newFake(t, "early")
	early.returnNil = true
	slow := newFake(t, "slow") // AcceptingReporter, not accepting yet

	g := mustGroup(t, bare{early}, slow)
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)

	eventually(t, func() bool { return slow.polls.Load() > 1 }, "readiness waiter never polled")
	eventually(t, func() bool { return !servingAt(g, 0) }, "bare Serve still running")
	slow.accepting.Store(true)
	polls := slow.polls.Load()
	eventually(t, func() bool { return slow.polls.Load() > polls }, "readiness waiter stopped polling")
	waitClosed(t, g.Ready(), false)
	if g.AllAccepting() {
		t.Error("AllAccepting = true with a stopped bare server")
	}

	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
	waitClosed(t, g.Ready(), false)
}

func TestGroupAllAcceptingFalseOnceShutdownBegins(t *testing.T) {
	unblock := make(chan struct{})
	s := newFake(t, "bare")
	s.onShutdown = func() { <-unblock }

	g := testGroup(WithShutdownTimeout(time.Second), WithShutdownGrace(100*time.Millisecond))
	if err := g.Add(bare{s}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	waitClosed(t, s.started, true)
	eventually(t, g.AllAccepting, "AllAccepting = false while serving")

	cancel()
	eventually(t, func() bool { return s.shutdowns.Load() == 1 }, "Shutdown not called")
	if !servingAt(g, 0) {
		t.Fatal("Serve returned before Shutdown was unblocked")
	}
	if g.AllAccepting() {
		t.Error("AllAccepting = true during shutdown")
	}

	close(unblock)
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestGroupAllAcceptingFalseAfterAbandoned(t *testing.T) {
	s := newFake(t, "stuck")
	s.stuck = true

	g := testGroup(WithShutdownTimeout(20*time.Millisecond), WithShutdownGrace(20*time.Millisecond))
	if err := g.Add(bare{s}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	waitClosed(t, s.started, true)
	cancel()

	if err := waitErr(t, ch); !errors.Is(err, ErrAbandoned) {
		t.Fatalf("Run = %v, want ErrAbandoned", err)
	}
	if !servingAt(g, 0) {
		t.Fatal("abandoned Serve not running")
	}
	if g.AllAccepting() {
		t.Error("AllAccepting = true with an abandoned Serve after Run returned")
	}
}

func TestRunServeGoexit(t *testing.T) {
	exiting := newFake(t, "exiting")
	exiting.goexit = true
	other := newFake(t, "other")

	g := mustGroup(t, bare{exiting}, other)
	err := waitErr(t, runAsync(t.Context(), g))
	if !errors.Is(err, errServeGoexit) || !strings.Contains(err.Error(), "exiting serve") {
		t.Fatalf("Run = %v, want exiting's Goexit error", err)
	}
	if servingAt(g, 0) {
		t.Error("serving still set after Goexit")
	}
	if other.shutdowns.Load() != 1 {
		t.Error("other server not shut down")
	}
	waitClosed(t, g.Ready(), false)
}

// panicky panics in IsAcceptingClients.
type panicky struct{ Server }

func (panicky) IsAcceptingClients() bool { panic("accepting boom") }

// readyPanicky panics in Ready.
type readyPanicky struct{ Server }

func (readyPanicky) Ready() <-chan struct{} { panic("ready boom") }

func TestGroupAllAcceptingPanic(t *testing.T) {
	s := newFake(t, "panicky")
	g := mustGroup(t, panicky{s})
	runServing(t, g, s.started)
	eventually(t, func() bool { return servingAt(g, 0) }, "Serve not running")
	if g.AllAccepting() {
		t.Error("AllAccepting = true, want false after IsAcceptingClients panic")
	}
}

// runServing runs g until the test ends, waiting for each started channel.
func runServing(t *testing.T, g *Group, started ...<-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	t.Cleanup(func() {
		cancel()
		if err := waitErr(t, ch); err != nil {
			t.Errorf("Run = %v", err)
		}
	})
	for _, c := range started {
		waitClosed(t, c, true)
	}
}

// fakeClock returns a clock for Group.now and a func advancing it.
func fakeClock() (now func() time.Time, advance func(time.Duration)) {
	var ns atomic.Int64
	ns.Store(time.Unix(1000, 0).UnixNano())
	return func() time.Time { return time.Unix(0, ns.Load()) },
		func(d time.Duration) { ns.Add(int64(d)) }
}

// syncBuffer is a bytes.Buffer safe for concurrent use as a log writer.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func TestPanicLogLimiterContention(t *testing.T) {
	const n = 64
	var l panicLogLimiter
	now := time.Unix(1_700_000_000, 0)

	var allowed, swapped atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if s, ok := l.allow(now); ok {
				allowed.Add(1)
				swapped.Add(s) // losers that raced the winner's swap
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := allowed.Load(); got != 1 {
		t.Fatalf("allowed = %d, want exactly 1", got)
	}
	suppressed, ok := l.allow(now.Add(acceptPanicLogInterval))
	if !ok {
		t.Fatal("allow after interval = false, want true")
	}
	if got := swapped.Load() + suppressed; got != n-1 {
		t.Errorf("suppressed total = %d, want %d (no count lost or duplicated)", got, n-1)
	}
}

// suppressedField extracts the "suppressed" count from a JSON log line.
func suppressedField(t *testing.T, line string) int {
	t.Helper()
	var v struct {
		Suppressed int `json:"suppressed"`
	}
	if err := json.Unmarshal([]byte(line), &v); err != nil {
		t.Fatalf("parse log line %q: %v", line, err)
	}
	return v.Suppressed
}

func TestGroupAllAcceptingPanicRateLimited(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap func(Server) Server
	}{
		{"IsAcceptingClients", func(s Server) Server { return panicky{s} }},
		{"Ready", func(s Server) Server { return readyPanicky{bare{s}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf syncBuffer
			now, advance := fakeClock()
			g := NewGroup(WithLogger(zerolog.New(&buf)))
			g.now = now
			s := newFake(t, "panicky")
			if err := g.Add(tc.wrap(s)); err != nil {
				t.Fatal(err)
			}
			runServing(t, g, s.started)
			eventually(t, func() bool { return servingAt(g, 0) }, "Serve not running")
			// AllAccepting's panic logs; the readiness waiter's own (single)
			// panic log from safeCall doesn't match.
			logged := func() []string {
				var lines []string
				for l := range strings.Lines(buf.String()) {
					if strings.Contains(l, tc.name+" panicked") && strings.Contains(l, "counts as not accepting") {
						lines = append(lines, l)
					}
				}
				return lines
			}
			probe := func() {
				if g.AllAccepting() {
					t.Error("AllAccepting = true, want false after a panic")
				}
			}

			const goroutines, calls = 8, 50
			var wg sync.WaitGroup
			for range goroutines {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for range calls {
						probe()
					}
				}()
			}
			wg.Wait()
			lines := logged()
			if len(lines) != 1 {
				t.Fatalf("panic logged %d times within one interval, want 1:\n%s", len(lines), strings.Join(lines, ""))
			}
			if !strings.Contains(lines[0], `"stack"`) {
				t.Errorf("first log = %s, want a stack", lines[0])
			}

			advance(acceptPanicLogInterval - 1)
			probe()
			if n := len(logged()); n != 1 {
				t.Fatalf("panic logged %d times before the interval passed, want 1", n)
			}

			advance(1)
			probe()
			lines = logged()
			if len(lines) != 2 {
				t.Fatalf("panic logged %d times after the interval, want 2", len(lines))
			}
			if !strings.Contains(lines[1], `"stack"`) {
				t.Errorf("second log = %s, want a stack", lines[1])
			}
			// Every call but the two logged ones is suppressed exactly once.
			// Under contention a loser's count can land in the first log (it
			// raced the winner's swap), so only the total is exact.
			if got, want := suppressedField(t, lines[0])+suppressedField(t, lines[1]), goroutines*calls; got != want {
				t.Errorf("suppressed total = %d, want %d", got, want)
			}
		})
	}
}

func TestGroupReadyPanicNeverReady(t *testing.T) {
	// A Ready panic in the readiness waiter must not crash the process; the
	// Group just never becomes ready.
	var buf syncBuffer
	s := newFake(t, "panicky")
	g := NewGroup(WithLogger(zerolog.New(&buf)))
	if err := g.Add(readyPanicky{bare{s}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	waitClosed(t, s.started, true)
	eventually(t, func() bool { return strings.Contains(buf.String(), "readiness check panicked: ready boom") },
		"readiness waiter panic not logged")
	waitClosed(t, g.Ready(), false)

	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
	waitClosed(t, g.Ready(), false)
}

func TestGroupAllAcceptingReporterFalseOnceShutdownBegins(t *testing.T) {
	unblock := make(chan struct{})
	s := newFake(t, "reporter")
	s.accepting.Store(true) // keeps reporting true during shutdown
	s.onShutdown = func() { <-unblock }

	g := testGroup(WithShutdownTimeout(time.Second), WithShutdownGrace(100*time.Millisecond))
	if err := g.Add(s); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	waitClosed(t, s.started, true)
	eventually(t, g.AllAccepting, "AllAccepting = false while serving")

	cancel()
	eventually(t, func() bool { return s.shutdowns.Load() == 1 }, "Shutdown not called")
	polls := s.polls.Load()
	if g.AllAccepting() {
		t.Error("AllAccepting = true during shutdown")
	}
	if s.polls.Load() != polls {
		t.Error("AllAccepting asked IsAcceptingClients during shutdown")
	}

	close(unblock)
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestGroupAllAcceptingNotifier(t *testing.T) {
	s := newFake(t, "notifier")
	n := notifier{bare{s}, make(chan struct{})}
	o := newFake(t, "other") // keeps the Group running once s stops

	g := mustGroup(t, n, bare{o})
	if g.AllAccepting() {
		t.Fatal("AllAccepting before Run = true, want false")
	}
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	waitClosed(t, s.started, true)
	waitClosed(t, o.started, true)
	if g.AllAccepting() {
		t.Error("AllAccepting = true before Ready closed")
	}

	close(n.ch)
	eventually(t, g.AllAccepting, "AllAccepting = false after Ready closed while serving")

	s.stopServe()
	eventually(t, func() bool { return !servingAt(g, 0) }, "Serve did not return")
	if g.stopping.Load() {
		t.Fatal("Group stopping after one nil Serve")
	}
	if g.AllAccepting() {
		t.Error("AllAccepting = true after Serve returned")
	}

	cancel()
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestGroupAllAcceptingNotifierFalseOnceShutdownBegins(t *testing.T) {
	unblock := make(chan struct{})
	s := newFake(t, "notifier")
	s.onShutdown = func() { <-unblock }
	n := notifier{bare{s}, make(chan struct{})}
	close(n.ch)

	g := testGroup(WithShutdownTimeout(time.Second), WithShutdownGrace(100*time.Millisecond))
	if err := g.Add(n); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ch := runAsync(ctx, g)
	waitClosed(t, s.started, true)
	eventually(t, g.AllAccepting, "AllAccepting = false while serving")

	cancel()
	eventually(t, func() bool { return s.shutdowns.Load() == 1 }, "Shutdown not called")
	if !servingAt(g, 0) {
		t.Fatal("Serve returned before Shutdown was unblocked")
	}
	if g.AllAccepting() {
		t.Error("AllAccepting = true during shutdown")
	}

	close(unblock)
	if err := waitErr(t, ch); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

var (
	_ Server            = (*fakeServer)(nil)
	_ AcceptingReporter = (*fakeServer)(nil)
	_ HealthReporter    = (*fakeServer)(nil)
	_ Server            = bare{}
)
