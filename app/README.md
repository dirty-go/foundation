# app

App bootstrap: app identity and config. It loads `Info` from env, loads
`.env` files, and derives the instance ID. Server lifecycle lives in
[`boot`](../boot/README.md).

## Overview

| API | Purpose |
|---|---|
| `Init(*Info)` / `InitByEnvDefault()` | Create the process-wide `App` (singleton). `InitByEnvDefault` reads `Info` from `APP_`-prefixed env vars. |
| `App.AppInfo()` / `App.InstanceID()` | The app's `Info` and this instance's ID (from `uname`). |
| `TZLocation()` / `Info.TZ()` | The location loaded from `Info.TZLocation` (falls back to `time.Local`). |
| `LoadEnv(keys...)` / `LoadEnvFrom(dir, keys...)` | Populate the process env from `<key>.env` files, inline env blobs, or `.env`. Never overrides existing vars. |
| `SetBuildInfo` / `GetBuildInfo` | Process-wide build metadata. |

## Example

```go
var log = logger.NewPkgLogger()

func main() {
	if err := run(); err != nil {
		log.Error().Err(err).Msg("exit")
		os.Exit(1)
	}
}

func run() error {
	a, err := app.InitByEnvDefault()
	if err != nil {
		return err
	}
	log.Info().Str("app", a.AppInfo().Name).Msg("starting")

	mux := http.NewServeMux()
	return boot.RunSignals(boot.NewHTTP("api", &http.Server{Addr: ":8080", Handler: mux}))
}
```

See the [`boot` README](../boot/README.md) for readiness, shutdown errors and
custom servers.

## Removed

**Breaking:** `app.ServiceServer`, `app.ReadyNotifier`, `app.HTTPServer`,
`app.NewHTTPServer`, `app.RunServers` and `App.AddServer`/`Run`/`Ready`/
`IsAllServersAcceptingClients` have been removed. Use `boot` directly; see
[CHANGELOG.md](../CHANGELOG.md) for the migration and the
[`boot` README](../boot/README.md).
