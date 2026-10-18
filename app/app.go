// Package app provides environment-loading helpers
package app

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/dirty-go/foundation/boot"
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
	EnvDefault                     = "dev"
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
	Env                     string `env:"ENV"`
	TZLocation              string `env:"TZ_LOCATION"`
	location                *time.Location
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

type App interface {
	AppInfo() Info
	InstanceID() string

	AddServer(boot.Server)
	Run()
	IsAllServersAcceptingClients() bool

	// Ready returns a channel that is closed once Run has started and every
	// server is accepting clients. It is never closed if a server fails to
	// start.
	Ready() <-chan struct{}
}

// Base implements App. Its zero value is usable. Server lifecycle is
// delegated to a boot.Group, so Base can run only once.
type Base struct {
	appInfo    Info
	instanceID string

	group   boot.Group
	servers atomic.Int32 // servers added; Run returns at once if none
}

// AppInfo returns the app's info.
func (appBase *Base) AppInfo() Info { return appBase.appInfo }

// InstanceID returns the ID of this running instance of the app.
func (appBase *Base) InstanceID() string { return appBase.instanceID }

// AddServer adds a server to be run simultaneously. It panics if srv is nil
// (including a typed nil) or if Run has already been called: both are wiring
// errors.
func (appBase *Base) AddServer(srv boot.Server) {
	// Count first so a racing Run never sees 0 while a server is registered;
	// undo the count if Add rejects srv.
	appBase.servers.Add(1)
	if err := appBase.group.Add(srv); err != nil {
		appBase.servers.Add(-1)
		panic(errors.Wrap("app: add server", err))
	}
}

// Run runs all the servers until SIGINT or SIGTERM. With no servers it
// returns at once; otherwise it blocks until a signal, even if every Serve
// returns nil. A serve failure shuts the other servers down gracefully, then
// exits the process via log.Fatal, as does a second call. Shutdown errors,
// including a shutdown abandoned at the deadline, are only logged. A second
// signal exits with 130/143; see boot.SignalContext.
func (appBase *Base) Run() {
	ctx, stop := boot.SignalContext(context.Background())
	defer stop()
	appBase.run(ctx)
}

func (appBase *Base) run(ctx context.Context) {
	checkRunErr(appBase.group.Run(ctx))
	if appBase.servers.Load() > 0 {
		waitSignal(ctx)
	}
}

// Ready implements App; see boot.Group.Ready.
func (appBase *Base) Ready() <-chan struct{} { return appBase.group.Ready() }

// IsAllServersAcceptingClients checks if every server is accepting clients.
func (appBase *Base) IsAllServersAcceptingClients() bool {
	return appBase.group.AllAccepting()
}

var (
	defApp     App
	defAppOnce sync.Once
)

func InitByEnvDefault() (App, error) {
	info := DefaultInfo()
	if err := loadInfoFromEnv(&info, env.Options{Prefix: EnvPrefixDefault}); err != nil {
		return nil, errors.Wrap("info loading from environment variables", err)
	}
	return Init(&info)
}

// loadInfoFromEnv overrides fields of info with the variables that are set;
// unset variables keep the values already in info.
func loadInfoFromEnv(info *Info, opts env.Options) error {
	return env.ParseWithOptions(info, opts)
}

func Init(info *Info) (App, error) {
	var err error
	defAppOnce.Do(func() {
		var appInfo Info
		if info != nil {
			appInfo = *info
		} else {
			appInfo = DefaultInfo()
		}

		var unameStr string
		unameStr, err = unameString()
		if err != nil {
			return
		}
		location, err := time.LoadLocation(appInfo.TZLocation)
		if err != nil {
			location = time.Local
		}
		if appInfo.Env == "" {
			appInfo.Env = "dev"
		}
		appInfo.location = location
		defApp = &Base{appInfo: appInfo, instanceID: unameStr}
	})

	if err != nil {
		return nil, err
	}

	return defApp, nil
}

func TZLocation() *time.Location { return defApp.AppInfo().location }

func (appInfo Info) TZ() *time.Location { return appInfo.location }
