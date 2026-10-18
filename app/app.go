// Package app provides environment-loading helpers
package app

import (
	"sync"
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

// InstanceID returns the ID of this running instance of the app.
func (appBase *Base) InstanceID() string { return appBase.instanceID }

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
