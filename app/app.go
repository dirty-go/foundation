// Package app provides environment-loading helpers
package app

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/dirty-go/foundation/errors"
)

const EnvPrefixDefault = "APP_"

// Default values used by DefaultInfo.
const (
	NameDefault                    = "Foundation"
	URLDefault                     = "https://github.com/dirty-go/foundation"
	EmailDefault                   = "nop@e-mail.mailer.com"
	NotificationEmailSenderDefault = "no-reply@e-mail.mailer.com"
	TeamNameDefault                = "Candi"
	EnvDefault                     = "dev" // used when APP_ENV is unset (fail-open); set APP_ENV in production
	DefaultTZLocation              = "UTC"
)

// Info describes the app. InitByEnvDefault loads it from APP_-prefixed
// environment variables; the env tag names keep the names previously derived
// by rez-go/stev.
type Info struct {
	// Name of the app
	Name string `env:"NAME"`
	// URL Canonical URL of the app
	URL                     string `env:"URL"`
	TermsOfServiceURL       string `env:"TERMS_OF_SERVICE_URL"`
	PrivacyPolicyURL        string `env:"PRIVACY_POLICY_URL"`
	Email                   string `env:"EMAIL"`
	NotificationEmailSender string `env:"NOTIFICATION_EMAIL_SENDER"`
	TeamName                string `env:"TEAM_NAME"`
	// Env is the deployment environment. Missing APP_ENV (or an empty Env
	// passed to Init) falls back to EnvDefault ("dev"): fail-open, so set
	// APP_ENV explicitly in production.
	Env        string `env:"ENV"`
	TZLocation string `env:"TZ_LOCATION"`
	location   *time.Location
}

func DefaultInfo() Info {
	return Info{
		Name:                    NameDefault,
		URL:                     URLDefault,
		Email:                   EmailDefault,
		NotificationEmailSender: NotificationEmailSenderDefault,
		TeamName:                TeamNameDefault,
		Env:                     EnvDefault,
		TZLocation:              DefaultTZLocation,
	}
}

// App is the process-wide app identity. Server lifecycle lives in package
// boot.
type App interface {
	AppInfo() Info
	InstanceID() string
}

// Base implements App.
type Base struct {
	appInfo    Info
	instanceID string
}

// AppInfo returns the app's info.
func (appBase *Base) AppInfo() Info { return appBase.appInfo }

// InstanceID returns the ID of this running instance of the app: 32
// lowercase hex chars, never empty. It is a truncated SHA-256 of a host
// identifier (the MAC of the first up interface with a MAC and a usable IPv4, else the
// first up non-loopback MAC, else the hostname), else a random per-process
// ID. See app/README.md for the exact derivation.
//
// The ID is pseudonymous, not anonymous: the MAC space is small enough to
// brute-force a MAC-derived ID, so don't treat it as a secret or an
// anonymising token. Uniqueness is best-effort (Docker bridge MACs repeat
// across hosts; hostnames collide under compose, hostNetwork and similar),
// and the ID can change across restarts when the interface set changes.
// Callers needing the raw MAC or hostname should read it themselves.
func (appBase *Base) InstanceID() string { return appBase.instanceID }

// errInitPanicked is Init's sticky error when the first call panicked and the
// caller recovered, so later calls never return (nil, nil).
var errInitPanicked = errors.New("app: Init panicked")

var (
	defApp     atomic.Pointer[Base] // published by Init, read by TZLocation
	errDefApp  error                // written once inside defAppOnce
	defAppOnce sync.Once
)

// InitByEnvDefault loads Info from APP_-prefixed environment variables over
// DefaultInfo and calls Init. A missing APP_ENV keeps EnvDefault ("dev"):
// this is fail-open, so set APP_ENV explicitly in production. Like Init, the
// first call wins; an env loading error is returned without consuming it.
func InitByEnvDefault() (App, error) {
	return initByEnv(env.Options{Prefix: EnvPrefixDefault})
}

func initByEnv(opts env.Options) (App, error) {
	info := DefaultInfo()
	if err := loadInfoFromEnv(&info, opts); err != nil {
		return nil, errors.Wrap("info loading from environment variables", err)
	}
	return Init(&info)
}

// loadInfoFromEnv overrides fields of info with the variables that are set;
// unset variables keep the values already in info.
func loadInfoFromEnv(info *Info, opts env.Options) error {
	return env.ParseWithOptions(info, opts)
}

// Init creates the process-wide App from info (DefaultInfo if nil).
//
// The first call wins: every later call, from Init or InitByEnvDefault,
// returns the same App, or the same error with a nil App, and ignores its
// info argument. Init never returns (nil, nil).
//
// An empty info.Env becomes EnvDefault ("dev"). An invalid info.TZLocation
// falls back to time.Local (unlike Info.TZ and TZLocation, which fall back
// to time.UTC when the location can't be loaded or Init hasn't run).
func Init(info *Info) (App, error) {
	return initApp(info, sysInstanceID)
}

// initApp is Init with the instance ID source injected (a test seam).
func initApp(info *Info, id func() (string, error)) (App, error) {
	defAppOnce.Do(func() {
		// Overwritten on a normal return. If newApp panics and the caller
		// recovers, the Once is spent: keep "never (nil, nil)" for later calls.
		errDefApp = errInitPanicked
		var b *Base
		b, errDefApp = newApp(info, id)
		defApp.Store(b)
	})
	if b := defApp.Load(); b != nil {
		return b, nil
	}
	return nil, errDefApp
}

func newApp(info *Info, instanceID func() (string, error)) (*Base, error) {
	appInfo := DefaultInfo()
	if info != nil {
		appInfo = *info
	}

	id, err := instanceID()
	if err != nil {
		return nil, errors.Wrap("app instance ID", err)
	}
	location, locErr := time.LoadLocation(appInfo.TZLocation)
	if locErr != nil {
		location = time.Local
	}
	if appInfo.Env == "" {
		appInfo.Env = EnvDefault
	}
	appInfo.location = location
	return &Base{appInfo: appInfo, instanceID: id}, nil
}

// TZLocation returns the App's location, or time.UTC before Init (or after a
// failed Init). It is safe to call concurrently with Init.
func TZLocation() *time.Location {
	if b := defApp.Load(); b != nil {
		return b.appInfo.TZ()
	}
	return time.UTC
}

// TZ returns the location loaded by Init. On an Info that didn't come from
// Init it loads TZLocation, returning time.UTC if that is empty or invalid;
// that lookup (a zoneinfo read) runs on every call and is not cached.
func (appInfo Info) TZ() *time.Location {
	if appInfo.location != nil {
		return appInfo.location
	}
	if appInfo.TZLocation != "" {
		if loc, err := time.LoadLocation(appInfo.TZLocation); err == nil {
			return loc
		}
	}
	return time.UTC
}
