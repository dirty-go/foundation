# app

App bootstrap: app identity and config. It loads `Info` from env, loads
`.env` files, and derives the instance ID. Server lifecycle lives in
[`boot`](../boot/README.md).

## Overview

| API | Purpose |
|---|---|
| `Init(*Info)` / `InitByEnvDefault()` | Create the process-wide `App` (singleton). `InitByEnvDefault` reads `Info` from `APP_`-prefixed env vars. |
| `App.AppInfo()` / `App.InstanceID()` | The app's `Info` and this instance's ID (see [Instance ID](#instance-id)). |
| `TZLocation()` / `Info.TZ()` | The location loaded from `Info.TZLocation` (see [Time zone](#time-zone)). |
| `LoadEnv(keys...)` / `LoadEnvFrom(dir, keys...)` | Populate the process env from `<key>.env` files, inline env blobs, or `.env`. Never overrides existing vars. |
| `SetBuildInfo` / `GetBuildInfo` | Process-wide build metadata. |

## Init: first call wins

`Init` and `InitByEnvDefault` share one `sync.Once`. The first call's `App`
**or** error sticks: every later call returns the same `App` (or the same
error with a nil `App`) and ignores its `Info`. `Init` never returns
`(nil, nil)`. An env parse error from `InitByEnvDefault` is returned before
`Init` runs, so it doesn't consume the first call.

## Env default

A missing `APP_ENV` (or an empty `Info.Env` passed to `Init`) falls back to
`EnvDefault` (`"dev"`). This is fail-open: set `APP_ENV` explicitly in
production.

## Instance ID

`InstanceID()` is never empty and is always 32 lowercase hex chars. It is
derived from the first of these that is available:

1. The MAC of the first interface that is up, has a MAC, and owns a
   non-loopback, non-link-local (not `169.254/16`) IPv4 address. MAC-less
   interfaces (tun, wireguard) are skipped, so a later `eth0` wins.
2. The MAC of the first interface that is up, not loopback, and has a MAC.
3. `os.Hostname()`, if it succeeds and is non-empty.
4. A random ID: 16 bytes from `crypto/rand`, hex-encoded, generated once per
   process. On Go ≥ 1.24 `crypto/rand.Reader` never returns an error; the
   pid+time fallback exists only defensively, for an injected reader.

Sources 1–3 are hashed, never returned raw:

```
hex(sha256("dirty-go/foundation/instance-id\x00" + kind + ":" + value)[:16])
```

`kind` is `mac` (value `aa:bb:cc:dd:ee:ff`, `net.HardwareAddr.String()`) or
`hostname`; it keeps a hostname from colliding with a MAC string. The random
ID is returned as is.

Caveats:

- **Pseudonymous, not anonymous, and not a secret.** The MAC space is small,
  so a MAC-derived ID can be brute-forced back to the MAC. Don't use it as a
  token or to anonymise anything. If you need the raw MAC or hostname, read it
  yourself.
- **Unsalted, so linkable.** Every service using this library on the same
  host reports the same ID.
- **Best-effort uniqueness.** Docker bridge MACs repeat across hosts, and
  hostnames collide under compose, `hostNetwork` and similar setups.
- **Not guaranteed stable.** It can change across restarts when the interface
  set changes, and the random fallback changes on every restart.

## Time zone

- After `Init`, `Info.TZ()` / `TZLocation()` return the location `Init`
  loaded from `Info.TZLocation`; an invalid zone falls back to `time.Local`.
- Before `Init` (or after a failed `Init`), `TZLocation()` returns
  `time.UTC`. It is safe to call concurrently with `Init`.
- `Info.TZ()` on an `Info` not returned by `Init` loads `TZLocation`,
  returning `time.UTC` if it is empty or invalid. Note the two fallbacks
  differ: `Init` uses `time.Local`, `Info.TZ()` uses `time.UTC`. This
  uninitialised path reads zoneinfo on every call (no caching), so avoid it
  on hot paths.

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
