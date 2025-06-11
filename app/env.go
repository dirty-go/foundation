package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

func LoadEnvFiles(envVarKeys []string, fileBasePath string) error {
	for _, k := range envVarKeys {
		str := os.Getenv(k)
		if str == "" {
			continue
		}
		r := strings.NewReader(str)
		envMap, err := godotenv.Parse(r)
		if err != nil {
			return err
		}
		for ik, iv := range envMap {
			if _, exists := os.LookupEnv(ik); !exists {
				_ = os.Setenv(ik, iv)
			}
		}
	}

	for _, k := range envVarKeys {
		envFile := filepath.Join(fileBasePath, k+".env")
		err := godotenv.Load(envFile)
		if err != nil {
			pathErr, _ := err.(*os.PathError)
			if pathErr == nil {
				return err
			}
		}
	}
	return nil
}
