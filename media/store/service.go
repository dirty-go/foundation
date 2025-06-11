package store

import (
	"bytes"
	"io"
)

type ServiceConfig any

type Service interface {
	PutObject(objectKey string, content io.Reader) (uploadInfo *UploadInfo, err error)
	GetObject(objectKey string) (stream *bytes.Buffer, err error)
	GetPublicObject(objectKey string) (string, error)
}
