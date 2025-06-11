package store

import (
	"github.com/rez-go/stev"
)

type Config struct {
	NameGenerationKey string `env:"FILENAME_GENERATION_KEY" yaml:"filename_generation_key" json:"filename_generation_key"`
	StoreService      string `env:"STORE_SERVICE" yaml:"store_service" json:"store_service"`

	Modules map[string]any `env:",map,squash" yaml:",inline,omitempty"`

	ImagesBaseURL string `env:"IMAGES_BASE_URL" yaml:"images_base_url" json:"images_base_url"`
}

// ParseConfigFromEnv populate the configuration by looking up the environment variables.
func ParseConfigFromEnv(prefix string) (cfg Config, err error) {
	cfg = ConfigSkeleton()
	err = stev.LoadEnv(prefix, &cfg)
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func ConfigSkeleton() Config {
	return Config{
		Modules: ModuleConfigSkeletons(),
	}
}
