# Foundation 

[![Go Reference](https://pkg.go.dev/badge/github.com/dirty-go/foundation.svg)](https://pkg.go.dev/github.com/dirty-go/foundation)

Bootstrapping service.

## Server lifecycle moved to `boot` (shims removed)

Running servers, readiness and graceful shutdown live in
[`boot`](boot/README.md). **Breaking:** the deprecated `app` shims
(`app.ServiceServer`, `app.ReadyNotifier`, `app.HTTPServer`,
`app.NewHTTPServer`, `app.RunServers`) and the `App.AddServer`/`Run`/`Ready`/
`IsAllServersAcceptingClients` methods have been removed; `app` is identity and
config only. The last release with the deprecated shims is `v0.1.0`. See
[CHANGELOG.md](CHANGELOG.md) for the migration.

## Env config migration (stev → caarlos0/env)

Env config is now loaded with
[`github.com/caarlos0/env/v11`](https://github.com/caarlos0/env) instead of
`github.com/rez-go/stev`. Variable names for `APP_`, `LOG_`, CORS and media
store config are unchanged. For downstream services:

- A nested struct tagged `env:"X_"` must use `envPrefix:"X_"` instead.
- Untagged fields are no longer loaded (stev derived a key from the field
  name); tag every field, including in custom media store module configs.
- Plain `int` fields are now supported.
- `envDefault` tags are now honoured.
- An empty bool variable (e.g. `MEDIA_MINIO_USE_SSL=`) is now unset/`false`;
  stev treated it as `true`. Affects gcs/minio `BUCKET_OPERATION` and minio
  `USE_SSL`. Set `true` explicitly.
- Media store module keys are unchanged (`<prefix><MODULE>_<KEY>`, e.g.
  `MEDIA_S3_REGION`). The module named by `STORE_SERVICE` is always loaded,
  so its required keys are enforced; any other module is loaded only when at
  least one of its declared keys is set.
- Required media store keys must now be non-empty, and s3
  `ACCESS_KEY_ID`/`SECRET_ACCESS_KEY` must be set together (or both left unset
  to use the AWS default credential chain).
- `Config.Modules` is tagged `env:"-"`: a struct embedding `store.Config` and
  parsed directly no longer gets module config; call
  `store.ParseConfigFromEnv(prefix)` instead.
