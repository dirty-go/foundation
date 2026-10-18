# app

App bootstrap: app info from env and `.env` loading. Server lifecycle lives
in [`boot`](../boot/README.md); `App.AddServer`/`Run`/`Ready` delegate to a
`boot.Group`.

## Overview

| API | Purpose |
|---|---|
| `Init(*Info)` / `InitByEnvDefault()` | Create the process-wide `App` (singleton). `InitByEnvDefault` reads `Info` from `APP_`-prefixed env vars. |
| `LoadEnv(keys...)` / `LoadEnvFrom(dir, keys...)` | Populate the process env from `<key>.env` files, inline env blobs, or `.env`. Never overrides existing vars. |
| `App.AddServer` / `App.Run` | Register `boot.Server`s, then run them on a `boot.Group` until `SIGINT`/`SIGTERM`. |
| `App.Ready()` / `App.IsAllServersAcceptingClients()` | `boot.Group.Ready` / `boot.Group.AllAccepting`. |

## Server lifecycle moved to `boot`

The runner, readiness and HTTP adapter moved from `app` to `boot`. Existing
code compiles unchanged: the old names are deprecated aliases/forwarders.

Before:

```go
func main() {
	a, err := app.InitByEnvDefault()
	if err != nil {
		panic(err)
	}
	a.AddServer(app.NewHTTPServer("api", &http.Server{Addr: ":8080", Handler: r}))
	a.Run()
}
```

After:

```go
var log = logger.NewPkgLogger() // github.com/dirty-go/foundation/logger

func main() {
	if _, err := app.InitByEnvDefault(); err != nil {
		panic(err)
	}
	if err := boot.RunSignals(
		boot.NewHTTP("api", &http.Server{Addr: ":8080", Handler: r}),
	); err != nil {
		log.Fatal().Err(err).Msg("servers")
	}
}
```

Unlike the shims, `boot.RunSignals`/`boot.Run`/`boot.Group.Run` return as
soon as every server has stopped (even without a signal) and return shutdown
errors as well as serve errors. Use `errors.As(err, new(*boot.ShutdownError))`
and `errors.Is(err, boot.ErrAbandoned)` to tell them apart.

Use a `boot.Group` (with `boot.SignalContext`) when you need options,
`Ready`, `Done` or `AllAccepting`; see the [`boot` README](../boot/README.md).

### Deprecated

| Deprecated | Use |
|---|---|
| `app.ServiceServer` | `boot.Server` (alias; existing implementations satisfy it) |
| `app.ReadyNotifier` | `boot.ReadyNotifier` (alias) |
| `app.HTTPServer` | `boot.HTTP` (alias) |
| `app.NewHTTPServer` | `boot.NewHTTP` |
| `app.RunServers` | `boot.RunSignals` (or `boot.Run` with your own context) |

`App.AddServer`/`Run`/`Ready`/`IsAllServersAcceptingClients` are kept and now
take `boot.Server`.

### Shim behaviour (`App.Run`, `RunServers`)

Kept from before:

- They block until `SIGINT`/`SIGTERM`, even if every `Serve` returns `nil`.
  With no servers they return at once.
- A `Shutdown` error is logged, not fatal (exit code unchanged).

Changed:

- A serve failure (or recovered `Serve` panic) now shuts the other servers
  down gracefully, then exits via `log.Fatal`; previously the process exited
  at once.
- Shutdown waits at most 15s timeout + 5s grace; stuck servers are logged
  (`boot.ErrAbandoned`) and `Run` returns instead of hanging. A `Shutdown`
  panic is recovered and logged like a `Shutdown` error.
- Forced shutdown (second signal) exits with 130 (`SIGINT`) / 143 (`SIGTERM`)
  instead of 0.
- `App.AddServer` panics on a nil server or after `Run`; `App.Run` can be
  called once (a second call is fatal).
- `NewHTTPServer` (= `boot.NewHTTP`) panics on a nil `*http.Server` and sets
  `ReadHeaderTimeout` to 10s if neither `ReadHeaderTimeout` nor `ReadTimeout`
  is set. Streaming/SSE services that left every timeout at 0 now get this
  header timeout (it bounds header reads only).

## Running servers with an App

`App.Run` blocks until `SIGINT`/`SIGTERM` (or exits on a serve failure), so
wait on `Ready` from a goroutine:

```go
go func() {
	<-a.Ready()
	log.Info().Msg("all servers are accepting clients")
}()
a.Run()
```

Every router below is an `http.Handler`, so they are all wired the same way.
Don't call the framework's own `Run`/`Start`/`ListenAndServe`, because the
`boot.Group` owns the server lifecycle.

### gin

```go
import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/dirty-go/foundation/app"
	"github.com/dirty-go/foundation/boot"
)

func main() {
	a, err := app.InitByEnvDefault()
	if err != nil {
		panic(err)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	a.AddServer(boot.NewHTTP("gin", &http.Server{
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

a.AddServer(boot.NewHTTP("chi", &http.Server{
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

a.AddServer(boot.NewHTTP("echo", &http.Server{
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

a.AddServer(boot.NewHTTP("restful", &http.Server{
	Addr:              ":8080",
	Handler:           c,
	ReadHeaderTimeout: 5 * time.Second,
}))
```

### net/http mux

```go
mux := http.NewServeMux()
mux.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("pong")) })

a.AddServer(boot.NewHTTP("http", &http.Server{
	Addr:              ":8080",
	Handler:           mux,
	ReadHeaderTimeout: 5 * time.Second,
}))
```

### Multiple servers

```go
a.AddServer(boot.NewHTTP("api", &http.Server{Addr: ":8080", Handler: apiRouter, ReadHeaderTimeout: 5 * time.Second}))
a.AddServer(boot.NewHTTP("admin", &http.Server{Addr: ":9090", Handler: adminMux, ReadHeaderTimeout: 5 * time.Second}))
a.Run()
```

Readiness probes can use `a.IsAllServersAcceptingClients()`.

Custom servers (gRPC, queue consumers) implement `boot.Server`; see the
[`boot` README](../boot/README.md#custom-servers).
