package rest

import (
	"testing"

	"github.com/caarlos0/env/v11"
)

func TestLoadCORSFilterConfig(t *testing.T) {
	ptr := func(s string) *string { return &s }
	tests := []struct {
		name string
		envs map[string]string
		want CORSFilterConfig
	}{
		{"unset", map[string]string{}, CORSFilterConfig{}},
		{"headers", map[string]string{"CORS_ALLOWED_HEADERS": "X-A, X-B"}, CORSFilterConfig{AllowedHeaders: ptr("X-A, X-B")}},
		{"methods", map[string]string{"CORS_ALLOWED_METHODS": "GET,POST"}, CORSFilterConfig{AllowedMethods: "GET,POST"}},
		{"domains", map[string]string{"CORS_ALLOWED_DOMAINS": "a.com"}, CORSFilterConfig{AllowedDomains: "a.com"}},
		{"other prefix ignored", map[string]string{"OTHER_ALLOWED_HEADERS": "X-A"}, CORSFilterConfig{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadCORSFilterConfig(env.Options{Prefix: "CORS_", Environment: tt.envs})
			if err != nil {
				t.Fatalf("loadCORSFilterConfig: %v", err)
			}
			if (got.AllowedHeaders == nil) != (tt.want.AllowedHeaders == nil) ||
				(got.AllowedHeaders != nil && *got.AllowedHeaders != *tt.want.AllowedHeaders) {
				t.Errorf("AllowedHeaders = %v, want %v", got.AllowedHeaders, tt.want.AllowedHeaders)
			}
			if got.AllowedMethods != tt.want.AllowedMethods || got.AllowedDomains != tt.want.AllowedDomains {
				t.Errorf("config = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadCORSFilterConfigEmptyHeaders(t *testing.T) {
	t.Run("options", func(t *testing.T) {
		got, err := loadCORSFilterConfig(env.Options{Prefix: "CORS_", Environment: map[string]string{"CORS_ALLOWED_HEADERS": ""}})
		if err != nil {
			t.Fatal(err)
		}
		if got.AllowedHeaders == nil || *got.AllowedHeaders != "" {
			t.Errorf("AllowedHeaders = %v, want pointer to empty string", got.AllowedHeaders)
		}
	})
	t.Run("os env", func(t *testing.T) {
		t.Setenv("CORSTEST_ALLOWED_HEADERS", "")
		got, err := loadCORSFilterConfig(env.Options{Prefix: "CORSTEST_"})
		if err != nil {
			t.Fatal(err)
		}
		if got.AllowedHeaders == nil || *got.AllowedHeaders != "" {
			t.Errorf("AllowedHeaders = %v, want pointer to empty string", got.AllowedHeaders)
		}
	})
}
