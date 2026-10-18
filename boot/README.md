# boot

Server lifecycle: run one or more servers, wait for readiness, shut down
gracefully on context cancellation or a serve failure. `SignalContext` turns
`SIGINT`/`SIGTERM` into cancellation. It doesn't depend on `app` and has no
global state (besides the signal handler `SignalContext` installs while in use).

## Overview

| API | Purpose |
|---|---|
| `Server` | Interface every runnable server implements: `ServerName`, `Serve`, `Shutdown`. |
| `ReadyNotifier` | Optional: `Ready() <-chan struct{}`, closed once the server accepts clients. |
| `AcceptingReporter` | Optional: `IsAcceptingClients() bool`. Polled by `Group.Ready` (without `ReadyNotifier`) and used by `Group.AllAccepting`. |
| `HealthReporter` | Optional: `IsHealthy() bool`. Not used by `Group`; type-assert for your own probes. |
| `NewHTTP(name, *http.Server)` | Adapts any `*http.Server` (and so any `http.Handler`) to `Server`. |
| `Run(ctx, servers...)` | Run until `ctx` is done or a server fails. Default options. |
| `RunSignals(servers...)` | `Run(SignalContext(context.Background()), servers...)`. For `main`. |
| `SignalContext(parent)` | Context cancelled by the first `SIGINT`/`SIGTERM`; a second signal exits 128+signum. |
| `NewGroup(opts...)` | `Add`, `Run(ctx)`, `Ready()`, `Done()`, `Err()`, `AllAccepting()`. |
| `WithShutdownTimeout(d)` / `WithShutdownGrace(d)` / `WithLogger(l)` | Group options. Defaults: 15s, 5s, `logger.NewPkgLogger()`. |

## Semantics

- Every server's `Serve` runs in its own goroutine.
- Shutdown starts when `ctx` is done **or** a `Serve` returns a non-nil error
  (a recovered panic counts). Every server then gets `Shutdown` concurrently,
  with a timeout context derived from `context.WithoutCancel(ctx)`.
- `Run` waits for every `Serve` and `Shutdown` to return, but at most shutdown
  timeout + grace. Past that it returns `ErrAbandoned` naming the stuck
  servers; their goroutines are abandoned and may outlive `Run`.
- `Run` returns the serve error(s) joined with any `Shutdown` errors, or `nil`
  after a clean shutdown caused by `ctx`. Each `Shutdown` error (or recovered
  `Shutdown` panic) is a `*ShutdownError`, so `errors.As` separates shutdown
  failures from serve failures. Errors carry the server name (and, for
  `HTTP`, the address). It never exits the process.
- Panics in `Serve`, `Shutdown` and `ServerName` are recovered into errors;
  the stack is logged.
- A `Serve` returning `nil` early doesn't stop the others (the group runs
  degraded). If every `Serve` returns `nil`, `Run` returns `nil` straight away
  and closes `Ready`.
- `Ready` never closes once shutdown has begun. It is a one-way latch: once
  closed it never reopens, even if a server stops later. Back readiness probes
  with `AllAccepting`, not `<-Ready()`.
- Readiness per server: `ReadyNotifier` if implemented, else
  `AcceptingReporter` (polled every 50ms), else ready while the group's call
  to its `Serve` is running. Every server is re-checked just before `Ready`
  closes: one whose `Serve` has returned (failed, or returned `nil` early,
  even after signalling ready) keeps `Ready` open.
- `AllAccepting` asks `AcceptingReporter` if implemented, also during and
  after shutdown (reporting false once it stops accepting is the server's
  job); a panic there counts as not accepting. It does **not** use
  `ReadyNotifier`: any other server counts as accepting while its `Serve` is
  running and shutdown hasn't begun — false before `Run`, from the moment
  `ctx` is done or a `Serve` fails, and after `Serve` returns, including an
  abandoned `Serve` after `Run` returned. An empty group is always accepting.
- The fallback for servers without readiness interfaces is optimistic: there is
  no signal that they have bound a listener. `Run` logs one Info line per such
  server. Implement `ReadyNotifier` or `AcceptingReporter` if that matters.
  A wrapper that embeds `boot.Server` hides them; forward them and assert
  with `var _ boot.ReadyNotifier = (*wrapper)(nil)`.
- A `Group` runs once: `Add` or `Run` after `Run` returns `ErrStarted`. `Add`
  rejects nil and typed-nil servers with `ErrNilServer`.
- `SignalContext`: a second signal calls `os.Exit(130|143)`, skipping defers.

## HTTP

`NewHTTP` binds `srv.Addr` (`:http`, i.e. port 80 on all interfaces, if empty)
before reporting ready, serves TLS when `srv.TLSConfig` is set (it must carry
certificates; validated before listening), and returns `nil` after `Shutdown`.
If neither `ReadHeaderTimeout` nor `ReadTimeout` is set, `NewHTTP` sets
`ReadHeaderTimeout = DefaultReadHeaderTimeout` (10s) **on your `*http.Server`**;
other timeouts are left to you. `Shutdown` falls back to `Close` when graceful
shutdown fails. `NewHTTP(name, nil)` panics.

## Example

Keep the work in a `run() error` so deferred cleanup (including `stop`) runs
before the process exits; `log.Fatal`/`os.Exit` skip defers.

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

	g := boot.NewGroup(boot.WithShutdownTimeout(10 * time.Second))
	if err := g.Add( // nil servers are rejected (ErrNilServer), not run
		boot.NewHTTP("api", &http.Server{Addr: ":8080", Handler: apiRouter}),
		boot.NewHTTP("admin", &http.Server{Addr: "127.0.0.1:9090", Handler: adminMux}),
	); err != nil {
		return err
	}

	go func() {
		select {
		case <-g.Ready():
			log.Info().Msg("all servers are accepting clients")
		case <-g.Done(): // startup failed; Ready never closes
		}
	}()

	err := g.Run(ctx)
	if err == nil && ctx.Err() == nil {
		// Every Serve returned nil before a signal: nothing is serving.
		log.Warn().Msg("all servers stopped before shutdown signal")
	}
	return err
}
```

`Run`'s error includes `Shutdown` failures (`*ShutdownError`) and
`ErrAbandoned`, not only serve failures. Exiting non-zero on any error is the
safe default; size `WithShutdownTimeout` + `WithShutdownGrace` to fit your
orchestrator's termination grace period so a slow drain isn't reported as a
failure. To treat shutdown-only failures as success, check that **every**
joined error is a `*ShutdownError` or wraps `ErrAbandoned` — a plain
`errors.As` also matches when a serve error is present.

## Routers

Every common router is an `http.Handler`, so wire it through `NewHTTP`. Don't
call the framework's own `Run`/`Start`/`ListenAndServe`; the group owns the
server lifecycle.

```go
r := gin.New() // or chi.NewRouter(), echo.New(), restful.NewContainer(), http.NewServeMux()
r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

if err := boot.RunSignals(boot.NewHTTP("api", &http.Server{
	Addr:              ":8080",
	Handler:           r,
	ReadHeaderTimeout: 5 * time.Second,
})); err != nil {
	log.Fatal().Err(err).Msg("servers")
}
```

## Custom servers

Implement `Server` for anything that isn't `net/http`, such as gRPC or
queue consumers. `Serve` must block until stopped and return `nil` on a
graceful `Shutdown`. `Shutdown` must make `Serve` return and must cope
with being called before `Serve` starts or after it returned.

Three methods are enough. Without a readiness interface the server counts as
ready and accepting while its `Serve` runs:

```go
type consumer struct {
	q    *queue.Client
	stop chan struct{} // closed by Shutdown
	once sync.Once
}

func (c *consumer) ServerName() string { return "consumer" }

func (c *consumer) Serve() error { return c.q.Consume(c.stop) } // returns nil once stop is closed

func (c *consumer) Shutdown(context.Context) error {
	c.once.Do(func() { close(c.stop) })
	return nil
}
```

Add the optional interfaces to report readiness precisely, e.g. only once the
listener is bound:

```go
type grpcServer struct {
	addr      string
	srv       *grpc.Server
	accepting atomic.Bool
}

var (
	_ boot.Server            = (*grpcServer)(nil)
	_ boot.AcceptingReporter = (*grpcServer)(nil)
	_ boot.HealthReporter    = (*grpcServer)(nil)
)

func (s *grpcServer) ServerName() string { return "grpc" }

func (s *grpcServer) Serve() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.accepting.Store(true)
	defer s.accepting.Store(false)
	return s.srv.Serve(ln) // returns nil after GracefulStop
}

func (s *grpcServer) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() { s.srv.GracefulStop(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.srv.Stop()
		return ctx.Err()
	}
}

// Optional: AcceptingReporter and HealthReporter.
func (s *grpcServer) IsAcceptingClients() bool { return s.accepting.Load() }
func (s *grpcServer) IsHealthy() bool          { return s.accepting.Load() }
```

To report something other than "listening" from `IsHealthy`, such as a DB
ping, embed `*boot.HTTP` and override `IsHealthy`.

### Calling readiness/health through `Server`

`Server` used to require `IsAcceptingClients` and `IsHealthy`. Implementations
still compile, but code that calls them on a `boot.Server` value must
type-assert:

```go
// Before
healthy := srv.IsHealthy()

// After
hr, ok := srv.(boot.HealthReporter)
healthy := ok && hr.IsHealthy() // pick your own default for !ok
```

## Migrating from `app`

The `app` shims (`app.ServiceServer`, `app.ReadyNotifier`, `app.HTTPServer`,
`app.NewHTTPServer`, `app.RunServers`) and `App.AddServer`/`Run`/`Ready`/
`IsAllServersAcceptingClients` have been removed; use this package directly.
See [CHANGELOG.md](../CHANGELOG.md) for the migration. The last commit with
the deprecated shims is a5da92e.
