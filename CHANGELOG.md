# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **BREAKING:** `boot.Group` no longer closes `Ready` when every `Serve`
  returns `nil`: nothing is serving, so `Ready` stays open and only `Done`
  closes. Code that waits on `<-g.Ready()` alone now blocks forever in that
  case: select on `Done` as well as `Ready` (as already required for startup
  failures), e.g. `select { case <-g.Ready(): ...; case <-g.Done(): ... }`. An
  empty `Group` still closes `Ready` when `Run` is called.
- **BREAKING:** `boot.Group.AllAccepting` returns `false` for a non-empty
  group once shutdown has begun, without calling `IsAcceptingClients`. A
  reporter that relied on being asked (or kept reporting `true`) during
  shutdown is no longer consulted.
- **BREAKING:** `boot.Group.AllAccepting` counts an `AcceptingReporter` as
  accepting only while its `Serve`, started by `Run`, is running (and
  shutdown hasn't begun), and doesn't call `IsAcceptingClients` otherwise.
  Previously the reporter alone decided, so one reporting `true` before `Run`,
  or after its `Serve` returned while the group ran degraded, made
  `AllAccepting` true. All three kinds of server (reporter, `ReadyNotifier`,
  neither) are now gated on `Serve` the same way. `*boot.HTTP` is unaffected.
- `boot.Group.AllAccepting` uses `ReadyNotifier` for servers without
  `AcceptingReporter`: such a server is accepting only once `Ready()` is
  closed while its `Serve` is running. Previously it counted as accepting as
  soon as its `Serve` started. Precedence: `AcceptingReporter`, then
  `ReadyNotifier`, then the `Serve`-running fallback.
- `boot.Group.AllAccepting` recovers a panic in `ReadyNotifier.Ready` as well
  as in `IsAcceptingClients`; either counts as not accepting. The panic is
  logged with its stack at most once a minute per server, with a
  `suppressed` count of the panics not logged since, so a probe can't flood
  the log. `Ready` must return the same channel every call, must not block
  and must not panic.
- `boot.NewHTTP` sets `IdleTimeout` to the new `boot.DefaultIdleTimeout`
  (120s) on the caller's `*http.Server` when both `IdleTimeout` and
  `ReadTimeout` are 0, bounding idle keep-alive connections. `ReadTimeout`
  stays opt-in because it would break streaming uploads, so slow request
  bodies and slow response reads remain unbounded by default: set
  `ReadTimeout`/`WriteTimeout`, or use `http.MaxBytesReader` and
  `http.ResponseController` deadlines per handler.
- `boot.Group.Run` called again (while running or after it returned) is
  documented to return `ErrStarted` without logging or exiting.
- **BREAKING:** `boot.Server` is now only `ServerName`, `Serve` and
  `Shutdown`. `IsAcceptingClients` and `IsHealthy` moved to the optional
  interfaces `boot.AcceptingReporter` and `boot.HealthReporter`, detected by
  type assertion like `boot.ReadyNotifier`. `*boot.HTTP` implements all three.
  - Types that **implement** `Server` keep compiling; the two methods are
    simply optional now.
  - Code that **calls** `IsAcceptingClients`/`IsHealthy` on a `boot.Server`
    value stops compiling and must type-assert. No such caller exists in this
    module outside `boot` itself.

    ```go
    // Before
    ok := srv.IsAcceptingClients()

    // After
    ar, isAR := srv.(boot.AcceptingReporter)
    ok := isAR && ar.IsAcceptingClients() // choose the default for !isAR
    ```
  - Behaviour: `Group.Ready` uses `ReadyNotifier`, else polls
    `IsAcceptingClients`, else treats the server as ready while its `Serve`
    is running (a server whose `Serve` has returned keeps `Ready` open).
    `Group.AllAccepting` uses `IsAcceptingClients` if implemented, else
    `ReadyNotifier`, in both cases only while the server's `Serve` is running
    (see the `AllAccepting` entries above). Custom servers that keep
    `IsAcceptingClients` behave as before apart from those entries; only
    servers that drop it get the optimistic
    fallback (no bound-listener signal), logged once at Info by `Run`.
  - `Group.AllAccepting` recovers a panic in `IsAcceptingClients` (counts as
    not accepting). A `Serve` that calls `runtime.Goexit` is reported as a
    serve error instead of hanging `Run`.

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
		case <-g.Done(): // startup failed, or every Serve returned; Ready never closes
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

### Fixed

- `errors.Arg(name, nil).Error()` no longer panics; it returns
  `"arg <name> invalid"`.
