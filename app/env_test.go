package app

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnvFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// unsetEnv clears key for the duration of the test, restoring it afterwards.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

func assertEnv(t *testing.T, key, want string) {
	t.Helper()
	got, ok := os.LookupEnv(key)
	if !ok {
		t.Fatalf("%s not set, want %q", key, want)
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

func assertUnset(t *testing.T, key string) {
	t.Helper()
	if v, ok := os.LookupEnv(key); ok {
		t.Fatalf("%s = %q, want unset", key, v)
	}
}

func TestLoadEnvFrom(t *testing.T) {
	const (
		key = "FOUNDATION_TEST_ENV_KEY"
		v1  = "FOUNDATION_TEST_V1"
		v2  = "FOUNDATION_TEST_V2"
	)

	t.Run("key file takes precedence over blob", func(t *testing.T) {
		dir := t.TempDir()
		writeEnvFile(t, dir, key+".env", v1+"=file\n")
		t.Setenv(key, v1+"=blob\n"+v2+"=blob\n")
		unsetEnv(t, v1)
		unsetEnv(t, v2)

		if err := LoadEnvFrom(dir, key); err != nil {
			t.Fatal(err)
		}
		assertEnv(t, v1, "file")
		assertUnset(t, v2)
	})

	t.Run("blob used when key file missing", func(t *testing.T) {
		dir := t.TempDir()
		writeEnvFile(t, dir, ".env", v2+"=dotenv\n")
		t.Setenv(key, v1+"=blob\n")
		unsetEnv(t, v1)
		unsetEnv(t, v2)

		if err := LoadEnvFrom(dir, key); err != nil {
			t.Fatal(err)
		}
		assertEnv(t, v1, "blob")
		assertUnset(t, v2) // .env skipped once a key resolved
	})

	t.Run("falls back to .env when no key resolves", func(t *testing.T) {
		dir := t.TempDir()
		writeEnvFile(t, dir, ".env", v1+"=dotenv\n")
		unsetEnv(t, key)
		unsetEnv(t, v1)

		if err := LoadEnvFrom(dir, key); err != nil {
			t.Fatal(err)
		}
		assertEnv(t, v1, "dotenv")
	})

	t.Run("falls back to .env with no keys", func(t *testing.T) {
		dir := t.TempDir()
		writeEnvFile(t, dir, ".env", v1+"=dotenv\n")
		unsetEnv(t, v1)

		if err := LoadEnvFrom(dir); err != nil {
			t.Fatal(err)
		}
		assertEnv(t, v1, "dotenv")
	})

	t.Run("missing everything is not an error", func(t *testing.T) {
		unsetEnv(t, key)
		if err := LoadEnvFrom(t.TempDir(), key); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("existing vars are not overridden", func(t *testing.T) {
		dir := t.TempDir()
		writeEnvFile(t, dir, key+".env", v1+"=file\n")
		t.Setenv(v1, "process")

		if err := LoadEnvFrom(dir, key); err != nil {
			t.Fatal(err)
		}
		assertEnv(t, v1, "process")

		t.Setenv(key+"_BLOB", v1+"=blob\n")
		if err := LoadEnvFrom(dir, key+"_BLOB"); err != nil {
			t.Fatal(err)
		}
		assertEnv(t, v1, "process")
	})

	t.Run("malformed blob returns error", func(t *testing.T) {
		t.Setenv(key, "export =\n'unterminated")
		if err := LoadEnvFrom(t.TempDir(), key); err == nil {
			t.Fatal("expected error")
		}
	})
}
