package logger

import (
	"strings"
	"testing"

	"github.com/caarlos0/env/v11"
)

func TestLoadConfigFromEnvNames(t *testing.T) {
	tests := []struct {
		key  string
		val  string
		want func(*Config)
	}{
		{"LOG_CONSOLE_LOG", "false", func(c *Config) { c.ConsoleLog = false }},
		{"LOG_ENCODE_LOGS_AS_JSON", "false", func(c *Config) { c.EncodeLogsAsJson = false }},
		{"LOG_FILE_LOGGING", "true", func(c *Config) { c.FileLogging = true }},
		{"LOG_DIRECTORY", "/tmp/logs", func(c *Config) { c.Directory = "/tmp/logs" }},
		{"LOG_FILENAME", "svc.log", func(c *Config) { c.Filename = "svc.log" }},
		{"LOG_MAX_SIZE", "100", func(c *Config) { c.MaxSize = 100 }},
		{"LOG_MAX_BACKUPS", "7", func(c *Config) { c.MaxBackups = 7 }},
		{"LOG_MAX_AGE", "90", func(c *Config) { c.MaxAge = 90 }},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, err := loadConfigFromEnv(env.Options{
				Prefix:      EnvPrefixDefault,
				Environment: map[string]string{tt.key: tt.val},
			})
			if err != nil {
				t.Fatalf("loadConfigFromEnv: %v", err)
			}
			want := configSkeletonPtr()
			tt.want(&want)
			if got != want {
				t.Errorf("config = %+v, want %+v", got, want)
			}
		})
	}
}

func TestLoadConfigFromEnvDefaults(t *testing.T) {
	tests := []struct {
		name string
		envs map[string]string
	}{
		{"unset", map[string]string{}},
		// Empty bool is treated as unset (stev treated it as true).
		{"empty bool", map[string]string{"LOG_FILE_LOGGING": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadConfigFromEnv(env.Options{Prefix: EnvPrefixDefault, Environment: tt.envs})
			if err != nil {
				t.Fatalf("loadConfigFromEnv: %v", err)
			}
			if want := configSkeletonPtr(); got != want {
				t.Errorf("config = %+v, want skeleton %+v", got, want)
			}
		})
	}
}

func TestLoadConfigFromEnvInvalid(t *testing.T) {
	_, err := loadConfigFromEnv(env.Options{
		Prefix:      EnvPrefixDefault,
		Environment: map[string]string{"LOG_MAX_SIZE": "big"},
	})
	if err == nil {
		t.Fatal("expected error for non-numeric LOG_MAX_SIZE")
	}
}

func TestDescribeConfigErrorOmitsValues(t *testing.T) {
	_, err := loadConfigFromEnv(env.Options{
		Prefix: EnvPrefixDefault,
		Environment: map[string]string{
			"LOG_MAX_SIZE":     "s3cr3t-size",
			"LOG_FILE_LOGGING": "s3cr3t-bool",
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid LOG_* values")
	}

	got := describeConfigError(err)
	for _, want := range []string{"MaxSize", "FileLogging"} {
		if !strings.Contains(got, want) {
			t.Errorf("describeConfigError() = %q, want it to name %s", got, want)
		}
	}
	if strings.Contains(got, "s3cr3t") {
		t.Errorf("describeConfigError() = %q, must not echo env values", got)
	}
}
