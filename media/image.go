package media

import (
	"io"

	"github.com/gabriel-vasile/mimetype"
)

var imageAllowedContentTypes = []string{
	"image/jpg",
	"image/jpeg",
	"image/png",
	"image/gif",
}

type imageMediaTypeInfo struct {
	mediaType     MediaType
	directoryName string
}

func (typeInfo *imageMediaTypeInfo) MediaType() MediaType {
	if typeInfo.mediaType == MediaType_MEDIA_TYPE_UNSPECIFIED {
		return MediaType_IMAGE
	}
	return typeInfo.mediaType
}

func (typeInfo *imageMediaTypeInfo) DirectoryName() string {
	if typeInfo.directoryName == "" {
		panic("directory name is unspecified")
	}
	return typeInfo.directoryName
}

func (typeInfo *imageMediaTypeInfo) IsContentTypeAllowed(contentType string) bool {
	for _, ct := range imageAllowedContentTypes {
		if ct == contentType {
			return true
		}
	}
	return false
}

func (typeInfo *imageMediaTypeInfo) DetectReader(r io.Reader) (*mimetype.MIME, error) {
	return mimetype.DetectReader(r)
}
