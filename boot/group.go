package boot

import (
	"context"
	"fmt"
	"reflect"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dirty-go/foundation/errors"
	"github.com/dirty-go/foundation/logger"
)

// readyPollInterval is how often Group.Ready polls an AcceptingReporter that
// doesn't implement ReadyNotifier.
const readyPollInterval = 50 * time.Millisecond

var (
	// ErrStarted is returned by Group.Add and Group.Run once Run has been
	// called.
	ErrStarted = errors.New("boot: group already started")
	// ErrNilServer is returned by Group.Add for a nil Server, including a
	// typed nil such as (*HTTP)(nil).
	ErrNilServer = errors.New("boot: nil server")
	// ErrAbandoned is returned (joined) by Group.Run when Serve or Shutdown
	// calls were still running at the hard shutdown deadline.
	ErrAbandoned = errors.New("boot: abandoned after shutdown deadline")

	errServeGoexit = errors.New("runtime.Goexit called in Serve")
)

// ShutdownError reports a failed (or panicking) Shutdown call in Group.Run's
// result. Use errors.As to tell shutdown failures apart from serve failures.
type ShutdownError struct {
	// Server is the failing server's name (its ServerName).
	Server string
	// Err is the non-nil error returned by Shutdown, or the recovered panic.
	Err error
}

// Error returns "<server> shutdown: <err>", or "<server> shutdown failed" if
// Err is nil (never the case for errors returned by Group.Run).
func (e *ShutdownError) Error() string {
	if e.Err == nil {
		return e.Server + " shutdown failed"
	}
	return e.Server + " shutdown: " + e.Err.Error()
}

// Unwrap returns e.Err.
func (e *ShutdownError) Unwrap() error { return e.Err }

// Group runs a set of servers together; see Group.Run. The zero value is
// usable with default options. A Group runs once.
type Group struct {
	cfg config

	initOnce sync.Once
	ready    chan struct{}
	done     chan struct{}
	err      error // written once before done is closed

	readyMu      sync.Mutex
	readyClosed  bool
	readyStopped bool // set when Run leaves its wait loop; ready can no longer close

	// stopping is set once shutdown begins (Run leaves its wait loop), before
	// any Shutdown call, and never cleared.
	stopping atomic.Bool

	mu      sync.Mutex
	servers []Server
	started bool
	// serving[i] is true while servers[i].Serve runs: set before Run starts
	// its goroutine, cleared when Serve returns (even after Run returned).
	// The slice is assigned under mu when Run starts and never resized.
	serving []atomic.Bool
}

// NewGroup returns a Group configured with opts.
func NewGroup(opts ...Option) *Group {
	g := &Group{}
	for _, o := range opts {
		o(&g.cfg)
	}
	g.init()
	return g
}

func (g *Group) init() {
	g.initOnce.Do(func() {
		g.ready = make(chan struct{})
		g.done = make(chan struct{})
	})
}

// Add registers servers to be run. It returns ErrStarted once Run has been
// called, and ErrNilServer (adding none of them) if any server is nil or a
// typed nil (a nil pointer, map, slice, func or chan in the interface).
func (g *Group) Add(servers ...Server) error {
	if slices.ContainsFunc(servers, isNil) {
		return ErrNilServer
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started {
		return ErrStarted
	}
	g.servers = append(g.servers, servers...)
	return nil
}

// isNil reports whether s is nil or holds a nil value, which would otherwise
// panic later inside a Run goroutine.
func isNil(s Server) bool {
	if s == nil {
		return true
	}
	switch v := reflect.ValueOf(s); v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}

// Ready returns a channel that is closed once Run has started and every
// server is accepting clients. A server is considered accepting once its
// ReadyNotifier channel closes; otherwise once its IsAcceptingClients
// (AcceptingReporter) reports true, polled; otherwise while its Serve, started
// by Run, is running. The last case is optimistic: there is no signal that
// such a server has bound its listener. Every server is re-checked just before
// Ready closes, so any server whose Serve has returned (even one that already
// signalled ready) keeps Ready open.
//
// Ready is never closed once shutdown has begun, so it stays open if a server
// fails to start; select on Done as well to avoid waiting forever. It is a
// one-way latch: once closed it never reopens, even if a server later stops.
// Use AllAccepting for a current answer, e.g. in a readiness probe.
//
// If every Serve returns nil (or the Group is empty), Run closes Ready, if
// still open, before Done: there is nothing left to wait for.
//
// If only some Serve calls return nil, the Group keeps running degraded and
// Ready may never close, since a stopped server no longer accepts clients.
func (g *Group) Ready() <-chan struct{} {
	g.init()
	return g.ready
}

// Done returns a channel that is closed when Run returns.
func (g *Group) Done() <-chan struct{} {
	g.init()
	return g.done
}

// Err returns Run's result once Done is closed, and nil before.
func (g *Group) Err() error {
	select {
	case <-g.Done():
		return g.err
	default:
		return nil
	}
}

// AllAccepting reports whether every server is accepting clients. It is true
// for an empty Group.
//
// A server implementing AcceptingReporter is asked directly, also during and
// after shutdown: reporting false once it stops accepting is its own
// responsibility. A panic in IsAcceptingClients is recovered, logged and
// counts as not accepting. Any other server, including one implementing only
// ReadyNotifier (which AllAccepting doesn't use), counts as accepting while
// its Serve, started by Run, is running and shutdown hasn't begun: not before
// Run, not once ctx is done or a Serve fails, and not after Serve returns.
// That is optimistic, since there is no signal
// that the server has bound its listener.
//
// It is safe to call concurrently with Add and Run.
func (g *Group) AllAccepting() bool {
	g.mu.Lock()
	servers := slices.Clone(g.servers)
	serving := g.serving
	g.mu.Unlock()
	for i, srv := range servers {
		if ar, ok := srv.(AcceptingReporter); ok {
			if !isAccepting(g.cfg.logger(), ar) {
				return false
			}
			continue
		}
		if serving == nil || !serving[i].Load() || g.stopping.Load() {
			return false
		}
	}
	return true
}

// isAccepting calls srv.IsAcceptingClients, treating a panic as false.
func isAccepting(log *logger.Logger, srv AcceptingReporter) (ok bool) {
	err := safeCall(log, fmt.Sprintf("%T", srv), "IsAcceptingClients", func() error {
		ok = srv.IsAcceptingClients()
		return nil
	})
	return ok && err == nil
}

// Run starts every server and blocks until they have all stopped.
//
// Shutdown is triggered when ctx is done or when a Serve returns a non-nil
// error (including a recovered panic). Run then calls Shutdown on every
// server concurrently, with a context derived from context.WithoutCancel(ctx)
// that expires after the shutdown timeout, and waits for every Serve and
// Shutdown to return.
//
// The wait is bounded: after the shutdown timeout plus the shutdown grace
// (WithShutdownGrace), Run stops waiting and returns an error wrapping
// ErrAbandoned that names the servers whose Serve or Shutdown hasn't
// returned. Their goroutines are abandoned and keep running until those
// calls return; they may outlive Run.
//
// A Serve that returns nil does not trigger shutdown: the other servers keep
// running and the group runs degraded. If every Serve returns nil there is
// nothing left to run, so Run returns nil without waiting for ctx and
// without calling Shutdown.
//
// A panic in Serve, Shutdown or ServerName is recovered and turned into an
// error naming the server and the panic value; the stack is logged. A
// ServerName panic fails Run before any server is started.
//
// The result joins the triggering serve error, any serve errors returned
// during shutdown, any Shutdown errors and ErrAbandoned. Errors are prefixed
// with the server name. Each Shutdown error (including a recovered Shutdown
// panic) is a *ShutdownError; serve errors are not, so callers can use
// errors.As to treat shutdown failures differently. It is nil after a clean
// shutdown caused by ctx. Run returns ErrStarted, without side effects, if
// called more than once.
func (g *Group) Run(ctx context.Context) error {
	g.init()
	g.mu.Lock()
	if g.started {
		g.mu.Unlock()
		return ErrStarted
	}
	g.started = true
	servers := slices.Clone(g.servers)
	g.serving = make([]atomic.Bool, len(servers))
	serving := g.serving
	g.mu.Unlock()

	err := g.run(ctx, servers, serving)
	g.err = err
	close(g.done)
	return err
}

type result struct {
	i   int
	err error
}

func (g *Group) run(ctx context.Context, servers []Server, serving []atomic.Bool) error {
	log := g.cfg.logger()
	names, err := serverNames(log, servers)
	if err != nil {
		return err
	}
	if len(servers) == 0 {
		g.closeReady(true)
		return nil
	}

	for i, srv := range servers {
		if !hasReadinessSignal(srv) {
			log.Info().Msgf("%s has no readiness signal (implements neither ReadyNotifier nor AcceptingReporter); "+
				"it counts as ready and accepting while its Serve runs", names[i])
		}
	}

	serveResults := make(chan result, len(servers))
	for i, srv := range servers {
		serving[i].Store(true)
		go func() {
			err := errServeGoexit // replaced unless Serve calls runtime.Goexit
			// Deferred so every exit path clears serving[i] before the
			// result is sent (defers run last-in, first-out).
			defer func() { serveResults <- result{i, err} }()
			defer serving[i].Store(false)
			log.Info().Msgf("Starting %s...", names[i])
			err = safeCall(log, names[i], "serve", srv.Serve)
		}()
	}

	// Started after every Serve goroutine, so waitReady may treat a server
	// without readiness interfaces as ready straight away.
	stopWaiter := make(chan struct{})
	waiterDone := make(chan struct{})
	go func() {
		defer close(waiterDone)
		_ = safeCall(log, "boot", "readiness check", func() error {
			if waitReady(stopWaiter, servers, serving) {
				g.closeReady(false)
			}
			return nil
		})
	}()

	var errs []error
	running := len(servers)
	collect := func(r result) {
		running--
		if r.err != nil {
			log.Err(r.err).Msgf("%s serve", names[r.i])
			errs = append(errs, errors.Wrap(names[r.i]+" serve", r.err))
			return
		}
		log.Info().Msgf("%s stopped", names[r.i])
	}

wait:
	for running > 0 {
		select {
		case <-ctx.Done():
			break wait
		case r := <-serveResults:
			collect(r)
			if r.err != nil {
				break wait
			}
		}
	}
	// Ready must not close, and fallback servers stop counting as accepting,
	// once shutdown begins (or after the last Serve).
	g.stopping.Store(true)
	g.stopReady()
	close(stopWaiter)

	shutdownResults := make(chan result, len(servers))
	shutdownErrs := make([]error, len(servers))
	shuttingDown := make([]bool, len(servers))
	pending := 0
	if running == 0 && len(errs) == 0 {
		g.closeReady(true) // every Serve returned nil
	} else {
		log.Info().Msg("Shutting down servers...")
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), g.cfg.timeout())
		defer cancel()
		for i, srv := range servers {
			shuttingDown[i] = true
			pending++
			go func() {
				log.Info().Msgf("Shutting down %s...", names[i])
				shutdownResults <- result{i, safeCall(log, names[i], "shutdown", func() error {
					return srv.Shutdown(shutdownCtx)
				})}
			}()
		}
	}

	deadline := time.NewTimer(g.cfg.timeout() + g.cfg.grace())
	defer deadline.Stop()
	waiter := waiterDone
	for running > 0 || pending > 0 || waiter != nil {
		select {
		case r := <-serveResults:
			collect(r)
		case r := <-shutdownResults:
			pending--
			shuttingDown[r.i] = false
			if r.err != nil {
				log.Err(r.err).Msgf("%s shutdown", names[r.i])
				shutdownErrs[r.i] = &ShutdownError{Server: names[r.i], Err: r.err}
			}
		case <-waiter:
			waiter = nil
		case <-deadline.C:
			stillServing := make([]bool, len(serving))
			for i := range serving {
				stillServing[i] = serving[i].Load()
			}
			abandoned := abandonedError(names, stillServing, shuttingDown)
			if abandoned != nil {
				log.Error().Err(abandoned).Msg("Shutdown deadline exceeded")
			} else {
				// Every Serve and Shutdown has returned; only uncollected
				// results or the readiness check remain.
				log.Warn().Msg("Shutdown deadline exceeded with results still pending; abandoned")
			}
			return errors.Join(append(append(errs, shutdownErrs...), abandoned)...)
		}
	}

	log.Info().Msg("Done.")
	return errors.Join(append(errs, shutdownErrs...)...)
}

// closeReady closes ready unless it is already closed or, without force,
// Run has left its wait loop.
func (g *Group) closeReady(force bool) {
	g.readyMu.Lock()
	defer g.readyMu.Unlock()
	if g.readyClosed || (g.readyStopped && !force) {
		return
	}
	g.readyClosed = true
	close(g.ready)
}

func (g *Group) stopReady() {
	g.readyMu.Lock()
	g.readyStopped = true
	g.readyMu.Unlock()
}

// serverNames calls ServerName on every server, turning panics into errors.
func serverNames(log *logger.Logger, servers []Server) ([]string, error) {
	names := make([]string, len(servers))
	var errs []error
	for i, srv := range servers {
		typ := fmt.Sprintf("%T", srv)
		if err := safeCall(log, typ, "ServerName", func() error {
			names[i] = srv.ServerName()
			return nil
		}); err != nil {
			errs = append(errs, errors.Wrap(typ+" ServerName", err))
		}
	}
	return names, errors.Join(errs...)
}

// abandonedError names the servers whose Serve or Shutdown is still running,
// or returns nil if there are none. The result wraps ErrAbandoned directly
// (errors.Unwrap returns it); app's deprecated shims rely on that shape.
func abandonedError(names []string, serving, shuttingDown []bool) error {
	var parts []string
	for _, call := range []struct {
		op      string
		running []bool
	}{{"serve", serving}, {"shutdown", shuttingDown}} {
		var stuck []string
		for i, r := range call.running {
			if r {
				stuck = append(stuck, names[i])
			}
		}
		if len(stuck) > 0 {
			parts = append(parts, call.op+": "+strings.Join(stuck, ", "))
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return fmt.Errorf("%w (%s)", ErrAbandoned, strings.Join(parts, "; "))
}

// safeCall calls f, converting a panic into an error. A panic with an error
// value is wrapped so errors.Is/As still work; any other value is reported by
// type only, since it may carry request data or credentials. The value and
// stack are logged, not returned.
func safeCall(log *logger.Logger, name, op string, f func() error) (err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		log.Error().Bytes("stack", debug.Stack()).Msgf("%s %s panicked: %v", name, op, r)
		if e, ok := r.(error); ok {
			err = errors.Wrap("panic", e)
			return
		}
		err = fmt.Errorf("panic: %T", r)
	}()
	return f()
}

// hasReadinessSignal reports whether srv implements ReadyNotifier or
// AcceptingReporter.
func hasReadinessSignal(srv Server) bool {
	switch srv.(type) {
	case ReadyNotifier, AcceptingReporter:
		return true
	default:
		return false
	}
}

// waitReady reports whether every server became ready: its ReadyNotifier
// channel closed, else its AcceptingReporter reported true (polled), else its
// Serve is still running (serving[i]). Once every signalling server is ready,
// every server's Serve is checked once more, so one that stopped in the
// meantime (including a signalling server that became ready and then returned
// nil) is caught. If one has stopped, or when stop is closed, waitReady waits
// for stop and returns false.
func waitReady(stop <-chan struct{}, servers []Server, serving []atomic.Bool) bool {
	if !waitSignalled(stop, servers) {
		return false
	}
	for i := range servers {
		if !serving[i].Load() {
			<-stop
			return false
		}
	}
	return true
}

// waitSignalled waits for every server implementing ReadyNotifier or
// AcceptingReporter to become ready. It returns false early when stop is
// closed.
func waitSignalled(stop <-chan struct{}, servers []Server) bool {
	for _, srv := range servers {
		switch s := srv.(type) {
		case ReadyNotifier:
			select {
			case <-s.Ready():
			case <-stop:
				return false
			}
		case AcceptingReporter:
			if !pollAccepting(stop, s) {
				return false
			}
		}
	}
	return true
}

func pollAccepting(stop <-chan struct{}, srv AcceptingReporter) bool {
	if srv.IsAcceptingClients() {
		return true
	}
	ticker := time.NewTicker(readyPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if srv.IsAcceptingClients() {
				return true
			}
		case <-stop:
			return false
		}
	}
}
