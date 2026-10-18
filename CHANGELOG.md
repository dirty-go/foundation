# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.2.0] - 2026-10-18

### Changed

- **BREAKING:** `App.InstanceID` is now always 32 lowercase hex chars and
  never exposes the raw MAC or hostname: it is
  `hex(sha256("dirty-go/foundation/instance-id\x00" + kind + ":" + value)[:16])`
  with `kind` `mac` or `hostname`, else a random per-process ID. The value
  changes on every host, so dashboards, metrics and anything else keyed on
  it will see new IDs. Sources, in order: the MAC of the first up interface
  that has a MAC and owns a non-loopback, non-link-local IPv4 address (read
  from the interface's own addresses; the old code took the *last* IPv4 and
  substring-matched it, so it could pick the wrong interface), else the first
  up non-loopback MAC, else the hostname, else the random ID. It is never
  empty. The ID is pseudonymous, not a secret, and unsalted (services on the
  same host share it); uniqueness is best-effort (see `app/README.md`).
- `app.Init` keeps the first call's `App` and error: every later call
  (including `InitByEnvDefault`) returns them and ignores its `Info`. A failed
  first call now returns its error on every later call instead of
  `(nil, nil)`.
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
  The last release with the deprecated shims is `v0.1.0`. Removed identifiers:
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

- `app.TZLocation()` returns `time.UTC` before `Init` and `Info.TZ()` loads
  `TZLocation` (or returns `time.UTC`) on an `Info` not from `Init`, instead
  of returning nil. `TZLocation()` no longer races with a concurrent first
  `Init`.
- `errors.Arg(name, nil).Error()` no longer panics; it returns
  `"arg <name> invalid"`.

## [v0.1.0] - 2026-10-18

First tagged release. It is the deprecation release for the `app` server
lifecycle; pin it while migrating to `boot`.

### Added

- `boot` package: runs servers with readiness, bounded graceful shutdown and
  signal handling (`Run`, `RunSignals`, `Group`, `NewHTTP`, `SignalContext`).
- `boot.ShutdownError`, so callers can tell shutdown failures from serve
  failures.
- `app.HTTPServer` adapter and `App.Ready` / `ReadyNotifier` readiness signal
  (deprecated in this same release in favour of `boot`).

### Changed

- Config for `app`, `logger`, REST/CORS and `media/store` modules is loaded
  with `caarlos0/env/v11`; the `stev` dependency is gone.
- Requires Go 1.24.

### Deprecated

- `app.ServiceServer`, `app.ReadyNotifier`, `app.HTTPServer`,
  `app.NewHTTPServer`, `app.RunServers` and the `App.AddServer`/`Run`/
  `Ready`/`IsAllServersAcceptingClients` lifecycle methods. They delegate to
  `boot` and were removed in `v0.2.0`.

### Fixed

- `errors.Ent`/`EntMsg` errors unwrap to their cause; `errors.Is`/`As` on them
  no longer loop forever.

[Unreleased]: https://github.com/dirty-go/foundation/compare/v0.2.0...HEAD
[v0.2.0]: https://github.com/dirty-go/foundation/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/dirty-go/foundation/releases/tag/v0.1.0
