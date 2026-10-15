package store_test

import (
	"strings"
	"testing"

	"github.com/dirty-go/foundation/media/store"
	"github.com/dirty-go/foundation/media/store/local"
	"github.com/dirty-go/foundation/media/store/minio"
	"github.com/dirty-go/foundation/media/store/s3"
)

const prefix = "MEDIA_"

func TestParseConfigFromEnvTopLevel(t *testing.T) {
	cfg, err := store.ParseConfigFromEnvWith(prefix, map[string]string{
		"MEDIA_FILENAME_GENERATION_KEY": "key",
		"MEDIA_STORE_SERVICE":           "local",
		"MEDIA_IMAGES_BASE_URL":         "https://img.example",
	})
	if err != nil {
		t.Fatalf("ParseConfigFromEnv: %v", err)
	}
	if cfg.NameGenerationKey != "key" || cfg.StoreService != "local" || cfg.ImagesBaseURL != "https://img.example" {
		t.Errorf("top-level fields = %+v", cfg)
	}
}

func TestParseConfigFromEnvModules(t *testing.T) {
	tests := []struct {
		name    string
		environ map[string]string
		wantErr string // substring; empty means no error
		check   func(t *testing.T, cfg store.Config)
	}{
		{
			name:    "no module vars set skips required",
			environ: map[string]string{},
			check: func(t *testing.T, cfg store.Config) {
				t.Helper()
				if got := moduleConfig[s3.Config](t, cfg, s3.ServiceName); got != (s3.Config{}) {
					t.Errorf("s3 config = %+v, want zero", got)
				}
				if got := moduleConfig[minio.Config](t, cfg, minio.ServiceName); got != (minio.Config{}) {
					t.Errorf("minio config = %+v, want zero", got)
				}
			},
		},
		{
			name: "module vars load under prefix and module name",
			environ: map[string]string{
				"MEDIA_S3_REGION":            "ap-southeast-1",
				"MEDIA_S3_BUCKET_NAME":       "bucket",
				"MEDIA_S3_ACCESS_KEY_ID":     "akid",
				"MEDIA_S3_SECRET_ACCESS_KEY": "secret",
			},
			check: func(t *testing.T, cfg store.Config) {
				t.Helper()
				want := s3.Config{
					Region:          "ap-southeast-1",
					BucketName:      "bucket",
					AccessKeyID:     "akid",
					SecretAccessKey: "secret",
				}
				if got := moduleConfig[s3.Config](t, cfg, s3.ServiceName); got != want {
					t.Errorf("s3 config = %+v, want %+v", got, want)
				}
				// minio has required fields but none set: not activated.
				if got := moduleConfig[minio.Config](t, cfg, minio.ServiceName); got != (minio.Config{}) {
					t.Errorf("minio config = %+v, want zero", got)
				}
			},
		},
		{
			name: "partially configured module names missing key",
			environ: map[string]string{
				"MEDIA_MINIO_ENDPOINT":      "localhost:9000",
				"MEDIA_MINIO_ACCESS_KEY_ID": "akid",
			},
			wantErr: "MEDIA_MINIO_SECRET_ACCESS_KEY",
		},
		{
			name:    "empty declared key still activates module",
			environ: map[string]string{"MEDIA_S3_ACCESS_KEY_ID": ""},
			wantErr: "MEDIA_S3_REGION",
		},
		{
			name:    "undeclared var with module prefix does not activate module",
			environ: map[string]string{"MEDIA_S3_FOO": "bar"},
			check: func(t *testing.T, cfg store.Config) {
				t.Helper()
				if got := moduleConfig[s3.Config](t, cfg, s3.ServiceName); got != (s3.Config{}) {
					t.Errorf("s3 config = %+v, want zero", got)
				}
			},
		},
		{
			name: "selected module is loaded even with no keys set",
			environ: map[string]string{
				"MEDIA_STORE_SERVICE": "minio",
			},
			wantErr: "MEDIA_MINIO_ACCESS_KEY_ID",
		},
		{
			name: "selected module without required keys loads with no keys set",
			environ: map[string]string{
				"MEDIA_STORE_SERVICE": "local",
			},
			check: func(t *testing.T, cfg store.Config) {
				t.Helper()
				if got := moduleConfig[local.Config](t, cfg, local.ServiceName); got != (local.Config{}) {
					t.Errorf("local config = %+v, want zero", got)
				}
			},
		},
		{
			name: "empty required key is rejected",
			environ: map[string]string{
				"MEDIA_S3_REGION":      "",
				"MEDIA_S3_BUCKET_NAME": "bucket",
			},
			wantErr: "MEDIA_S3_REGION",
		},
		{
			name: "empty bool is false",
			environ: map[string]string{
				"MEDIA_MINIO_ENDPOINT":          "localhost:9000",
				"MEDIA_MINIO_ACCESS_KEY_ID":     "akid",
				"MEDIA_MINIO_SECRET_ACCESS_KEY": "secret",
				"MEDIA_MINIO_USE_SSL":           "",
			},
			check: func(t *testing.T, cfg store.Config) {
				t.Helper()
				if got := moduleConfig[minio.Config](t, cfg, minio.ServiceName); got.UseSSL {
					t.Errorf("minio UseSSL = true, want false")
				}
			},
		},
		{
			name: "several modules configured at once",
			environ: map[string]string{
				"MEDIA_S3_REGION":               "ap-southeast-1",
				"MEDIA_S3_BUCKET_NAME":          "bucket",
				"MEDIA_MINIO_ENDPOINT":          "localhost:9000",
				"MEDIA_MINIO_ACCESS_KEY_ID":     "akid",
				"MEDIA_MINIO_SECRET_ACCESS_KEY": "secret",
			},
			check: func(t *testing.T, cfg store.Config) {
				t.Helper()
				if got := moduleConfig[s3.Config](t, cfg, s3.ServiceName); got.Region != "ap-southeast-1" {
					t.Errorf("s3 Region = %q, want ap-southeast-1", got.Region)
				}
				if got := moduleConfig[minio.Config](t, cfg, minio.ServiceName); got.Endpoint != "localhost:9000" {
					t.Errorf("minio Endpoint = %q, want localhost:9000", got.Endpoint)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := store.ParseConfigFromEnvWith(prefix, tt.environ)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("err = nil, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseConfigFromEnv: %v", err)
			}
			tt.check(t, cfg)
		})
	}
}

func TestParseConfigFromEnvProcessEnv(t *testing.T) {
	t.Setenv("MEDIA_STORE_SERVICE", "s3")
	t.Setenv("MEDIA_S3_REGION", "ap-southeast-1")
	t.Setenv("MEDIA_S3_BUCKET_NAME", "bucket")

	cfg, err := store.ParseConfigFromEnv(prefix)
	if err != nil {
		t.Fatalf("ParseConfigFromEnv: %v", err)
	}
	if got := moduleConfig[s3.Config](t, cfg, s3.ServiceName); got.BucketName != "bucket" {
		t.Errorf("s3 BucketName = %q, want bucket", got.BucketName)
	}
}

func moduleConfig[T any](t *testing.T, cfg store.Config, name string) T {
	t.Helper()
	c, ok := cfg.Modules[name].(*T)
	if !ok {
		t.Fatalf("module %s config is %T, want *%T", name, cfg.Modules[name], *new(T))
	}
	return *c
}
