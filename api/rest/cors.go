package rest

import (
	"strings"

	"github.com/emicklei/go-restful/v3"
	"github.com/rez-go/stev"
	"github.com/timemore/foundation/errors"
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
	var cfg CORSFilterConfig
	err := stev.LoadEnv(envPrefix, &cfg)
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
