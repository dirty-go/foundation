# app

App bootstrap and server lifecycle: app info from env, `.env` loading, and
running one or more servers with graceful shutdown.

## Overview

| API | Purpose |
|---|---|
| `Init(*Info)` / `InitByEnvDefault()` | Create the process-wide `App` (singleton). `InitByEnvDefault` reads `Info` from `APP_`-prefixed env vars. |
| `LoadEnv(keys...)` / `LoadEnvFrom(dir, keys...)` | Populate the process env from `<key>.env` files, inline env blobs, or `.env`. Never overrides existing vars. |
| `ServiceServer` | Interface every runnable server implements. |
| `NewHTTPServer(name, *http.Server)` | Adapts any `*http.Server` (and so any `http.Handler`) to `ServiceServer`. |
| `App.AddServer` / `App.Run` | Register servers, then run them all until `SIGINT`/`SIGTERM`. |
| `RunServers([]ServiceServer)` | What `App.Run` calls; usable without an `App`. |

## Running servers

`Run` starts every server in its own goroutine and blocks until `SIGINT` or
`SIGTERM`. It then calls `Shutdown` on every server, with a 15s timeout in
total, and waits for them to stop. A second signal forces the process to exit
immediately. If `Serve` returns a non-nil error, `log.Fatal` ends the process.

`NewHTTPServer` handles the `net/http` details:

- `Serve` binds `srv.Addr` (`:http` if empty), then serves. It returns `nil`
  after `Shutdown`, so a graceful stop is not treated as a fatal error.
- If `srv.TLSConfig` is set, it serves TLS. The certificates must come from
  `TLSConfig.Certificates` or `GetCertificate`.
- `IsAcceptingClients` / `IsHealthy` are true while the listener is bound.
- You build the `*http.Server`, so you control the timeouts. Always set
  `ReadHeaderTimeout`, at least.

Every router below is an `http.Handler`, so they are all wired the same way.
Don't call the framework's own `Run`/`Start`/`ListenAndServe`, because
`App.Run` owns the server lifecycle.

### gin

```go
import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/dirty-go/foundation/app"
)

func main() {
	a, err := app.InitByEnvDefault()
	if err != nil {
		panic(err)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	a.AddServer(app.NewHTTPServer("gin", &http.Server{
		Addr:              ":8080",
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}))
	a.Run()
}
```

### chi

```go
r := chi.NewRouter() // github.com/go-chi/chi/v5
r.Use(middleware.Recoverer)
r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("pong")) })

a.AddServer(app.NewHTTPServer("chi", &http.Server{
	Addr:              ":8080",
	Handler:           r,
	ReadHeaderTimeout: 5 * time.Second,
}))
```

### echo

```go
e := echo.New() // github.com/labstack/echo/v4
e.Use(middleware.Recover())
e.GET("/ping", func(c echo.Context) error { return c.String(http.StatusOK, "pong") })

a.AddServer(app.NewHTTPServer("echo", &http.Server{
	Addr:              ":8080",
	Handler:           e,
	ReadHeaderTimeout: 5 * time.Second,
}))
```

### go-restful

```go
ws := new(restful.WebService) // github.com/emicklei/go-restful/v3
ws.Route(ws.GET("/ping").To(func(_ *restful.Request, resp *restful.Response) {
	_, _ = resp.Write([]byte("pong"))
}))

c := restful.NewContainer()
c.Add(ws)

a.AddServer(app.NewHTTPServer("restful", &http.Server{
	Addr:              ":8080",
	Handler:           c,
	ReadHeaderTimeout: 5 * time.Second,
}))
```

### net/http mux

```go
mux := http.NewServeMux()
mux.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("pong")) })

a.AddServer(app.NewHTTPServer("http", &http.Server{
	Addr:              ":8080",
	Handler:           mux,
	ReadHeaderTimeout: 5 * time.Second,
}))
```

### Multiple servers

Servers run side by side and shut down together, for example a public API
plus an internal admin/metrics listener:

```go
a.AddServer(app.NewHTTPServer("api", &http.Server{Addr: ":8080", Handler: apiRouter, ReadHeaderTimeout: 5 * time.Second}))
a.AddServer(app.NewHTTPServer("admin", &http.Server{Addr: ":9090", Handler: adminMux, ReadHeaderTimeout: 5 * time.Second}))
a.Run()
```

Readiness probes can use `a.IsAllServersAcceptingClients()`.

### Without an App

```go
app.RunServers([]app.ServiceServer{
	app.NewHTTPServer("api", srv),
})
```

## Custom servers

Implement `ServiceServer` for anything that isn't `net/http`, such as gRPC or
queue consumers. `Serve` must block until stopped and return `nil` on a
graceful `Shutdown`.

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
ping, embed `*app.HTTPServer` and override `IsHealthy`.
