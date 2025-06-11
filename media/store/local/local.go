package local

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"github.com/timemore/foundation/errors"
	mediastore "github.com/timemore/foundation/media/store"
	"gopkg.in/yaml.v3"
)

type Config struct {
	DirectoryPath string `env:"FOLDER_PATH" yaml:"folder_path" json:"folder_path"`
}

const ServiceName = "local"

func init() {
	mediastore.RegisterModule(
		ServiceName,
		mediastore.Module{
			NewService: NewService,
			ServiceConfigSkeleton: func() mediastore.ServiceConfig {
				cfg := ConfigSkeleton()
				return &cfg
			},
		})
}

func NewService(config mediastore.ServiceConfig) (mediastore.Service, error) {
	if config == nil {
		return nil, errors.ArgMsg("config", "missing")
	}

	conf, ok := config.(*Config)
	if !ok {
		b, _ := yaml.Marshal(config)
		var cfg Config
		err := yaml.Unmarshal(b, &cfg)
		if err != nil {
			return nil, errors.ArgMsg("config", "type invalid")
		}
		conf = &cfg
	}

	return &Service{
		directoryPath: conf.DirectoryPath,
	}, nil
}

type Service struct {
	directoryPath string
}

func (s *Service) PutObject(objectKey string, contentSource io.Reader) (uploadInfo *mediastore.UploadInfo, err error) {
	if s.directoryPath != "" {
		targetName := filepath.Join(s.directoryPath, objectKey)
		targetFile, err := os.Create(targetName)
		if err != nil {
			return nil, errors.Wrap("create file", err)
		}
		defer func() {
			_ = targetFile.Close()
		}()

		_, err = io.Copy(targetFile, contentSource)
		if err != nil {
			return nil, errors.Wrap("write content", err)
		}
	}
	stream := &bytes.Buffer{}
	buf := make([]byte, 1*1024*1024)
	dataSize := 0
	for {
		n, err := contentSource.Read(buf)
		if err != nil && err != io.EOF {
			panic(err)
		}
		dataSize += n

		_, _ = stream.Write(buf)
		if err == io.EOF || n == 0 {
			break
		}
	}

	return &mediastore.UploadInfo{
		Bucket: s.directoryPath,
		Key:    objectKey,
		Output: stream,
		Size:   dataSize,
	}, nil
}

func (s *Service) GetPublicObject(sourceKey string) (targetURL string, err error) {
	// TODO: final URL! ask the HTTP server to provide this.
	return
}

func (s *Service) GetObject(sourceKey string) (stream *bytes.Buffer, err error) {
	sourceFile := filepath.Join(s.directoryPath, sourceKey)
	if _, err := os.Stat(sourceFile); os.IsNotExist(err) {
		return nil, errors.EntMsg(sourceFile, err.Error())
	}
	f, err := os.Open(sourceFile)
	if err != nil {
		return nil, errors.Wrap("open file", err)
	}

	buf := make([]byte, 1*1024*1024)
	dataSize := 0
	for {
		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			panic(err)
		}
		dataSize += n

		_, _ = stream.Write(buf)
		if err == io.EOF || n == 0 {
			break
		}
	}

	return
}

var _ mediastore.Service = &Service{}

func ConfigSkeleton() Config { return Config{} }

type UploadOutput struct {
	Directory string
	Key       string
	Output    *bytes.Buffer
	Size      int
}
