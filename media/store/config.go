package store

import (
	"os"
	"sort"
	"strings"

	"github.com/caarlos0/env/v11"

	"github.com/dirty-go/foundation/errors"
)

type Config struct {
	NameGenerationKey string `env:"FILENAME_GENERATION_KEY" yaml:"filename_generation_key" json:"filename_generation_key"`
	StoreService      string `env:"STORE_SERVICE" yaml:"store_service" json:"store_service"`

	// Modules is loaded per module by ParseConfigFromEnv, not by the env tag.
	Modules map[string]any `env:"-" yaml:",inline,omitempty"`

	ImagesBaseURL string `env:"IMAGES_BASE_URL" yaml:"images_base_url" json:"images_base_url"`
}

// ParseConfigFromEnv populates the configuration by looking up the environment variables.
//
// Top-level fields are read from <prefix><KEY>. Each registered module's
// config is read from <prefix><UPPER(module name)>_<KEY>. The module named by
// StoreService is always loaded, so its required fields are enforced. Any
// other module is only loaded, and its required fields only enforced, when at
// least one of its declared keys is set; otherwise its skeleton is left
// untouched.
func ParseConfigFromEnv(prefix string) (Config, error) {
	return parseConfigFromEnv(prefix, env.ToMap(os.Environ()))
}

func parseConfigFromEnv(prefix string, environ map[string]string) (Config, error) {
	cfg := ConfigSkeleton()
	if err := env.ParseWithOptions(&cfg, env.Options{Prefix: prefix, Environment: environ}); err != nil {
		return Config{}, err
	}

	names := make([]string, 0, len(cfg.Modules))
	for name := range cfg.Modules {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		opts := env.Options{
			Prefix:      prefix + strings.ToUpper(name) + "_",
			Environment: environ,
		}
		modCfg := cfg.Modules[name]
		if name != cfg.StoreService {
			active, errActive := anyKeySet(modCfg, opts)
			if errActive != nil {
				return Config{}, errors.Wrap("media store module "+name, errActive)
			}
			if !active {
				continue
			}
		}
		if errParse := env.ParseWithOptions(modCfg, opts); errParse != nil {
			return Config{}, errors.Wrap("media store module "+name, errParse)
		}
	}

	return cfg, nil
}

// anyKeySet reports whether any env key declared by cfg is present in
// opts.Environment, even with an empty value.
func anyKeySet(cfg any, opts env.Options) (bool, error) {
	params, err := env.GetFieldParamsWithOptions(cfg, opts)
	if err != nil {
		return false, err
	}
	for _, p := range params {
		if p.Ignored {
			continue
		}
		if _, ok := opts.Environment[p.Key]; ok {
			return true, nil
		}
	}
	return false, nil
}

func ConfigSkeleton() Config {
	return Config{
		Modules: ModuleConfigSkeletons(),
	}
}
