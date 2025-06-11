package app

import (
	"sync"
	"time"

	"github.com/rez-go/stev"
	"github.com/timemore/foundation/errors"
)

const EnvPrefixDefault = "APP_"

const (
	NameDefault                    = "Timemore"
	URLDefault                     = "https://github.com/timemore"
	EmailDefault                   = "nop@e-mail.mailer.com"
	NotificationEmailSenderDefault = "no-reply@e-mail.mailer.com"
	TeamNameDefault                = "Time More Team's"
	EnvDefault                     = "dev"
	DefaultTZLocation              = "UTC"
)

type Info struct {
	// Name of the app
	Name string
	// URL Canonical URL of the app
	URL                     string
	TermsOfServiceURL       string
	PrivacyPolicyURL        string
	Email                   string
	NotificationEmailSender string
	TeamName                string
	Env                     string
	TZLocation              string
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

	AddServer(ServiceServer)
	Run()
	IsAllServersAcceptingClients() bool
}

type Base struct {
	appInfo    Info
	instanceID string

	servers   []ServiceServer
	serversMu sync.Mutex
}

func (appBase Base) AppInfo() Info      { return appBase.appInfo }
func (appBase Base) InstanceID() string { return appBase.instanceID }

// AddServer adds a server to be run simultaneously. Do NOT call this
// method after the app has been started.
func (appBase *Base) AddServer(srv ServiceServer) {
	appBase.serversMu.Lock()
	appBase.servers = append(appBase.servers, srv)
	appBase.serversMu.Unlock()
}

// Run runs all the servers. Do NOT add any new server after this method was called.
func (appBase *Base) Run() {
	RunServers(appBase.servers)
}

// IsAllServersAcceptingClients checks if every server is accepting clients.
func (appBase *Base) IsAllServersAcceptingClients() bool {
	servers := appBase.servers
	for _, srv := range servers {
		if !srv.IsAcceptingClients() {
			return false
		}
	}
	return true
}

var (
	defApp     App
	defAppOnce sync.Once
)

func InitByEnvDefault() (App, error) {
	info := DefaultInfo()
	err := stev.LoadEnv(EnvPrefixDefault, &info)
	if err != nil {
		return nil, errors.Wrap("info loading from environment variables", err)
	}
	return Init(&info)
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
