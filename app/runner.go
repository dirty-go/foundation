package app

import (
	"context"

	"github.com/dirty-go/foundation/boot"
	"github.com/dirty-go/foundation/errors"
	"github.com/dirty-go/foundation/logger"
)

var log = logger.NewPkgLogger()

// RunServers runs servers until SIGINT or SIGTERM, like App.Run: it returns
// at once for no servers, otherwise blocks until a signal even if every Serve
// returns nil. A serve failure shuts the other servers down and exits the
// process (log.Fatal); shutdown errors, including a shutdown abandoned at the
// deadline, are only logged. A second signal exits with 130/143.
//
// Deprecated: use boot.RunSignals, which returns all errors (shutdown ones
// included) and returns once every server has stopped.
func RunServers(servers []ServiceServer) {
	if len(servers) == 0 {
		return
	}
	ctx, stop := boot.SignalContext(context.Background())
	defer stop()
	runServers(ctx, servers)
}

func runServers(ctx context.Context, servers []ServiceServer) {
	g := boot.NewGroup()
	if err := g.Add(servers...); err != nil {
		log.Fatal().Err(err).Msg("run servers")
	}
	checkRunErr(g.Run(ctx))
	waitSignal(ctx)
}

// waitSignal blocks until ctx is done, warning if the servers have already
// stopped (every Serve returned nil, or only shutdown errors) without it.
func waitSignal(ctx context.Context) {
	if ctx.Err() == nil {
		log.Warn().Msg("all servers stopped before shutdown signal; waiting for signal")
	}
	<-ctx.Done()
}

// checkRunErr handles boot.Group.Run's result for the deprecated shims,
// keeping the pre-boot behaviour: shutdown failures are logged, anything else
// exits the process. boot has already logged each failure in full.
func checkRunErr(err error) {
	if err == nil {
		return
	}
	if !shutdownOnly(err) {
		log.Fatal().Err(err).Msg("run servers")
	}
	log.Error().Msg("servers stopped with shutdown errors")
}

// shutdownOnly reports whether err is shutdown-only: a non-empty join (or
// nested joins) whose every leaf is a direct *boot.ShutdownError or boot's
// abandoned error. A stuck shutdown used to hang rather than exit, so
// abandonment is not treated as fatal.
//
// It is deliberately structural rather than using errors.As/Is: Run wraps
// each serve error with the server name, so a serve error whose chain holds a
// ShutdownError or ErrAbandoned (e.g. a wrapped nested Group's result) would
// be misclassified as shutdown-only, and walking arbitrary user Unwrap chains
// can be unbounded. Only joins are recursed into; any other leaf is fatal
// unless it is exactly what boot produces.
func shutdownOnly(err error) bool {
	if err == nil {
		return false
	}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		errs := j.Unwrap()
		for _, e := range errs {
			if !shutdownOnly(e) {
				return false
			}
		}
		return len(errs) > 0
	}
	if _, ok := err.(*boot.ShutdownError); ok { //nolint:errorlint // direct leaf only; see above
		return true
	}
	// boot builds the abandoned leaf as fmt.Errorf("%w (...)", ErrAbandoned).
	return err == boot.ErrAbandoned || errors.Unwrap(err) == boot.ErrAbandoned //nolint:errorlint // direct leaf only; see above
}
