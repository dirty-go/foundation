# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Removed

- **BREAKING:** the server-lifecycle shims in `app`, deprecated in TASK-004.
  The last commit with the deprecated shims is a5da92e. Removed identifiers:
  - `app.ServiceServer` → `boot.Server`
  - `app.ReadyNotifier` → `boot.ReadyNotifier`
  - `app.HTTPServer` → `boot.HTTP`
  - `app.NewHTTPServer` → `boot.NewHTTP`
  - `app.RunServers` → `boot.RunSignals` (or `boot.Run` with your own context)
  - `App.AddServer`, `App.Run`, `App.Ready`, `App.IsAllServersAcceptingClients`
    (and the same methods on `app.Base`) → `boot.RunSignals` or a `boot.Group`
- `app.App` and `app.Base` are now identity/config only: `AppInfo()` and
  `InstanceID()`. `app` no longer imports `boot`. Code that *calls* the removed
  methods on an `App` stops compiling. Types that *implement* `App` (fakes,
  generated mocks) still satisfy it, but their extra lifecycle methods are now
  dead — regenerate mocks and drop expectations on `AddServer`/`Run`/`Ready`.

Migration, simple case:

```go
// Before
a.AddServer(app.NewHTTPServer("api", srv))
a.Run()

// After
if err := boot.RunSignals(boot.NewHTTP("api", srv)); err != nil {
	log.Fatal().Err(err).Msg("servers")
}
```

When you need `Ready`/`IsAllServersAcceptingClients`, use a `boot.Group`. Keep
the work in a `run() error` so deferred cleanup runs before exiting
(`log.Fatal` skips defers):

```go
func main() {
	if err := run(); err != nil {
		log.Error().Err(err).Msg("servers")
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := boot.SignalContext(context.Background())
	defer stop()

	g := boot.NewGroup()
	if err := g.Add(boot.NewHTTP("api", srv)); err != nil { // was: AddServer panicked
		return err
	}
	go func() {
		select {
		case <-g.Ready(): // was a.Ready()
			log.Info().Msg("all servers are accepting clients")
		case <-g.Done(): // startup failed; Ready never closes
		}
	}()
	// Readiness probes: g.AllAccepting() (was a.IsAllServersAcceptingClients()).
	return g.Run(ctx)
}
```

Behaviour differences from the removed shims:

- `boot` returns errors instead of exiting the process.
- Shutdown failures are returned too (`*boot.ShutdownError`,
  `boot.ErrAbandoned`); the shims only logged them and exited 0. Exiting
  non-zero on them is the safer default, but under rolling deploys size the
  shutdown timeout to your termination grace period, or classify them as in
  the [`boot` README](boot/README.md#example).
- `Run` returns once every server has stopped instead of blocking until a
  signal, and without the shims' Warn: check `ctx.Err() == nil` after `Run`
  if an early stop should be logged or treated as a failure.
- `Group.Add` returns `ErrNilServer`/`ErrStarted` instead of panicking — check
  the error, or a rejected batch silently runs nothing.
