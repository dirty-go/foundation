package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"

	"github.com/dirty-go/foundation/errors"
)

// LoadEnvFiles is LoadEnvFrom with the arguments in the legacy order.
func LoadEnvFiles(envVarKeys []string, fileBasePath string) error {
	return LoadEnvFrom(fileBasePath, envVarKeys...)
}

// LoadEnv populates the process environment for the given keys, resolving
// files against the current working directory. Called with no keys, it
// simply loads the root .env file. See LoadEnvFrom for the full resolution
// order.
func LoadEnv(keys ...string) error {
	return LoadEnvFrom(".", keys...)
}

// LoadEnvFrom is LoadEnv with an explicit base directory to resolve env
// files against, primarily useful in tests.
//
// For each key, in order:
//  1. A key-specific file at <fileBasePath>/<key>.env is loaded if present.
//  2. Otherwise, a process env var named <key> is treated as an inline
//     block of "NAME=value" lines (e.g. a CI secret holding a whole env
//     file) and parsed.
//
// If none of the keys resolved via either a file or a parameter (including
// when no keys are given at all), LoadEnvFrom falls back to a single
// <fileBasePath>/.env file.
//
// Variables already set in the process environment are never overridden.
func LoadEnvFrom(fileBasePath string, keys ...string) error {
	var loadedAny bool

	for _, key := range keys {
		loaded, err := loadKey(fileBasePath, key)
		if err != nil {
			return err
		}
		loadedAny = loadedAny || loaded
	}

	if loadedAny {
		return nil
	}

	return loadFile(filepath.Join(fileBasePath, ".env"))
}

// loadKey resolves a single key via its dedicated file, then via an inline
// env-blob parameter, reporting whether either source was found.
func loadKey(fileBasePath, key string) (bool, error) {
	path := filepath.Join(fileBasePath, key+".env")
	switch err := godotenv.Load(path); {
	case err == nil:
		return true, nil
	case !errors.Is(err, os.ErrNotExist):
		return false, errors.Wrap("loading "+path, err)
	}

	blob := os.Getenv(key)
	if blob == "" {
		return false, nil
	}

	parsed, err := godotenv.Parse(strings.NewReader(blob))
	if err != nil {
		return false, errors.Wrap("parsing "+key+" env parameter", err)
	}

	for k, v := range parsed {
		if _, exists := os.LookupEnv(k); !exists {
			if err := os.Setenv(k, v); err != nil {
				return false, errors.Wrap("setting "+k, err)
			}
		}
	}

	return true, nil
}

func loadFile(path string) error {
	if err := godotenv.Load(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Wrap("loading "+path, err)
	}
	return nil
}
