package boot

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/dirty-go/foundation/errors"
	"github.com/dirty-go/foundation/logger"
)

const (
	// DefaultShutdownTimeout is the default total time servers get to shut
	// down.
	DefaultShutdownTimeout = 15 * time.Second
	// DefaultShutdownGrace is the default extra time Run waits for Serve and
	// Shutdown to return after the shutdown timeout before abandoning them.
	DefaultShutdownGrace = 5 * time.Second
)

// Option configures a Group.
type Option func(*config)

type config struct {
	shutdownTimeout time.Duration
	shutdownGrace   time.Duration
	log             *logger.Logger
	logOnce         sync.Once
}

// WithShutdownTimeout sets the total time servers get to shut down. A
// non-positive d keeps DefaultShutdownTimeout.
func WithShutdownTimeout(d time.Duration) Option {
	return func(c *config) { c.shutdownTimeout = d }
}

// WithShutdownGrace sets how long Run keeps waiting, after the shutdown
// timeout, for Serve and Shutdown calls to return before abandoning them; see
// Group.Run. A non-positive d keeps DefaultShutdownGrace.
func WithShutdownGrace(d time.Duration) Option {
	return func(c *config) { c.shutdownGrace = d }
}

// WithLogger sets the logger for lifecycle events. The default is a
// logger.NewPkgLogger logger, created when the Group runs.
func WithLogger(l logger.Logger) Option {
	return func(c *config) { c.log = &l }
}

func (c *config) timeout() time.Duration {
	if c.shutdownTimeout <= 0 {
		return DefaultShutdownTimeout
	}
	return c.shutdownTimeout
}

func (c *config) grace() time.Duration {
	if c.shutdownGrace <= 0 {
		return DefaultShutdownGrace
	}
	return c.shutdownGrace
}

// logger returns the configured logger, creating the default on first use.
func (c *config) logger() *logger.Logger {
	c.logOnce.Do(func() {
		if c.log == nil {
			l := logger.NewPkgLogger().Logger
			c.log = &l
		}
	})
	return c.log
}

// Run runs servers with default options until ctx is done or a server fails;
// see Group.Run. Use a Group for options or readiness.
func Run(ctx context.Context, servers ...Server) error {
	g := NewGroup()
	if err := g.Add(servers...); err != nil {
		return err
	}
	return g.Run(ctx)
}

// RunSignals runs servers with default options until SIGINT or SIGTERM. It is
// Run with a SignalContext; meant for main.
func RunSignals(servers ...Server) error {
	ctx, stop := SignalContext(context.Background())
	defer stop()
	return Run(ctx, servers...)
}

// exit is os.Exit, replaced in tests.
var exit = os.Exit

// SignalContext returns a copy of parent that is cancelled on the first
// SIGINT or SIGTERM; context.Cause reports the signal. A second signal exits
// the process immediately with code 128+signal (130 for SIGINT, 143 for
// SIGTERM), skipping deferred calls; use it to escape a stuck shutdown.
//
// It installs a process-wide handler for SIGINT and SIGTERM (signal.Notify)
// until the returned stop function is called. stop cancels the context,
// restores default signal handling (unless other handlers remain), and
// releases every goroutine SignalContext started. Call it as soon as the
// context is no longer needed, typically with defer.
func SignalContext(parent context.Context) (ctx context.Context, stop context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case s := <-sig:
			cancel(errors.New("boot: received " + s.String()))
		case <-quit:
			return
		}
		select {
		case s := <-sig:
			exit(exitCode(s))
		case <-quit:
		}
	}()

	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			signal.Stop(sig)
			close(quit)
			<-done
			cancel(nil)
		})
	}
}

// exitCode returns the shell convention 128+signum for s.
func exitCode(s os.Signal) int {
	if n, ok := s.(syscall.Signal); ok {
		return 128 + int(n)
	}
	return 1
}
