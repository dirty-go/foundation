package s3

import (
	"bytes"
	"io"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/timemore/foundation/errors"
	mediastore "github.com/timemore/foundation/media/store"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Region          string `env:"REGION,required" yaml:"region" json:"region"`
	BucketName      string `env:"BUCKET_NAME,required" yaml:"bucket_name" json:"bucket_name"`
	AccessKeyID     string `env:"ACCESS_KEY_ID" yaml:"access_key_id" json:"access_key_id"`
	SecretAccessKey string `env:"SECRET_ACCESS_KEY" yaml:"secret_access_key" json:"secret_access_key"`
}

const ServiceName = "s3"

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

func ConfigSkeleton() Config { return Config{} }

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
	if conf == nil || conf.Region == "" || conf.BucketName == "" {
		return nil, errors.ArgMsg("config", "fields invalid")
	}

	var creds *credentials.Credentials
	if conf.AccessKeyID != "" {
		creds = credentials.NewStaticCredentials(
			conf.AccessKeyID,
			conf.SecretAccessKey,
			"",
		)
	}

	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(conf.Region),
		Credentials: creds,
	})
	if err != nil {
		return nil, errors.Wrap("AWS Session", err)
	}

	const uploadPartSize = 10 * 1024 * 1024 // 10MiB

	return &Service{
		bucketName: conf.BucketName,
		uploader: s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
			u.PartSize = uploadPartSize
		}),
		downloader: s3manager.NewDownloader(sess),
		svc:        s3.New(sess),
	}, nil
}

type Service struct {
	bucketName string
	uploader   *s3manager.Uploader
	svc        *s3.S3
	downloader *s3manager.Downloader
}

func (s *Service) PutObject(targetKey string, contentSource io.Reader) (uploadInfo *mediastore.UploadInfo, err error) {
	result, err := s.uploader.Upload(&s3manager.UploadInput{
		Body:   contentSource,
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(targetKey),
	})
	if err != nil {
		return nil, errors.Wrap("upload", err)
	}

	return &mediastore.UploadInfo{
		Bucket:   *aws.String(s.bucketName),
		Key:      *aws.String(targetKey),
		UploadID: result.UploadID,
		Location: result.Location,
	}, nil
}

func (s *Service) GetPublicObject(sourceKey string) (targetURL string, err error) {
	req, _ := s.svc.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(sourceKey),
	})
	targetURL, err = req.Presign(15 * time.Minute)

	if err != nil {
		return "", err
	}

	return targetURL, nil
}

func (s *Service) GetObject(sourceKey string) (stream *bytes.Buffer, err error) {
	buff := &aws.WriteAtBuffer{}
	_, err = s.downloader.Download(buff,
		&s3.GetObjectInput{
			Bucket: aws.String(s.bucketName),
			Key:    aws.String(sourceKey),
		})
	if err != nil {
		return nil, err
	}
	buf := new(bytes.Buffer)
	buf.Read(buff.Bytes())
	return buf, nil
}

var _ mediastore.Service = &Service{}
