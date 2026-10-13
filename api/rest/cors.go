package rest

import (
	"os"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/emicklei/go-restful/v3"

	"github.com/dirty-go/foundation/errors"
)

// CORSFilterConfig get cors filter from env
// AllowedHeaders define which headers is allowed
// AllowedMethods define which http methods is allowed
// AllowedDomains define which domain is allowd or set to (*)
type CORSFilterConfig struct {
	AllowedHeaders *string `env:"ALLOWED_HEADERS"`
	AllowedMethods string  `env:"ALLOWED_METHODS"`
	AllowedDomains string  `env:"ALLOWED_DOMAINS"`
}

func SetupCORSFilterByEnv(restContainer *restful.Container, envPrefix string) error {
	cfg, err := loadCORSFilterConfig(env.Options{Prefix: envPrefix})
	if err != nil {
		return errors.Wrap("config loading from environment variables", err)
	}

	var allowedHeaders []string
	if cfg.AllowedHeaders != nil {
		if strVal := *cfg.AllowedHeaders; strVal != "" {
			parts := strings.Split(strVal, ",")
			for _, str := range parts {
				allowedHeaders = append(allowedHeaders, strings.TrimSpace(str))
			}
		}
	} else {
		allowedHeaders = []string{"Content-Type", "Accept", "Authorization"}
	}

	var allowedMethods []string
	if strVal := cfg.AllowedMethods; strVal != "" {
		parts := strings.Split(strVal, ",")
		for _, str := range parts {
			allowedMethods = append(allowedMethods, strings.TrimSpace(str))
		}
	}

	var allowedDomains []string
	if strVal := cfg.AllowedDomains; strVal != "" {
		parts := strings.Split(strVal, ",")
		for _, str := range parts {
			allowedDomains = append(allowedDomains, strings.TrimSpace(str))
		}
	} else {
		allowedDomains = []string{"*"}
	}

	restContainer.Filter(restful.CrossOriginResourceSharing{
		AllowedHeaders: allowedHeaders,
		AllowedDomains: allowedDomains,
		AllowedMethods: allowedMethods,
		CookiesAllowed: false,
		Container:      restContainer,
	}.Filter)
	return nil
}

// loadCORSFilterConfig leaves AllowedHeaders nil when the variable is unset.
// env treats an empty value as unset, so an explicitly empty ALLOWED_HEADERS
// (meaning "allow no headers") is allocated here to keep the stev behaviour.
func loadCORSFilterConfig(opts env.Options) (CORSFilterConfig, error) {
	var cfg CORSFilterConfig
	if err := env.ParseWithOptions(&cfg, opts); err != nil {
		return cfg, err
	}
	if cfg.AllowedHeaders == nil {
		key := opts.Prefix + "ALLOWED_HEADERS"
		var set bool
		if opts.Environment != nil {
			_, set = opts.Environment[key]
		} else {
			_, set = os.LookupEnv(key)
		}
		if set {
			cfg.AllowedHeaders = new(string)
		}
	}
	return cfg, nil
}
