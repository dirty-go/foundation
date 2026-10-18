# boot

Server lifecycle: run one or more servers, wait for readiness, shut down
gracefully on context cancellation or a serve failure. `SignalContext` turns
`SIGINT`/`SIGTERM` into cancellation. It doesn't depend on `app` and has no
global state (besides the signal handler `SignalContext` installs while in use).

## Overview

| API | Purpose |
|---|---|
| `Server` | Interface every runnable server implements. |
| `ReadyNotifier` | Optional: `Ready() <-chan struct{}`, closed once the server accepts clients. |
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
- `Ready` never closes once shutdown has begun.
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

```go
func main() {
	ctx, stop := boot.SignalContext(context.Background())
	defer stop()

	g := boot.NewGroup(boot.WithShutdownTimeout(10 * time.Second))
	_ = g.Add(
		boot.NewHTTP("api", &http.Server{Addr: ":8080", Handler: apiRouter}),
		boot.NewHTTP("admin", &http.Server{Addr: ":9090", Handler: adminMux}),
	)

	go func() {
		select {
		case <-g.Ready():
			log.Info().Msg("all servers are accepting clients")
		case <-g.Done(): // startup failed; Ready never closes
		}
	}()

	if err := g.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("servers")
	}
}
```

## Custom servers

Implement `Server` for anything that isn't `net/http`, such as gRPC or
queue consumers. `Serve` must block until stopped and return `nil` on a
graceful `Shutdown`. `Shutdown` must make `Serve` return and must cope
with being called before `Serve` starts or after it returned.

```go
type grpcServer struct {
	addr      string
	srv       *grpc.Server
	accepting atomic.Bool
}

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

func (s *grpcServer) IsAcceptingClients() bool { return s.accepting.Load() }
func (s *grpcServer) IsHealthy() bool          { return s.accepting.Load() }
```

To report something other than "listening" from `IsHealthy`, such as a DB
ping, embed `*boot.HTTP` and override `IsHealthy`.

## Migrating from `app`

`app.ServiceServer`, `app.ReadyNotifier`, `app.HTTPServer`,
`app.NewHTTPServer` and `app.RunServers` are deprecated aliases/forwarders
for this package; see the [`app` README](../app/README.md#server-lifecycle-moved-to-boot)
for the migration and behaviour changes.
