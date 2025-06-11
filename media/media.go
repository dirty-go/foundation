package media

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"sync/atomic"

	"github.com/gabriel-vasile/mimetype"
	"github.com/nfnt/resize"
)

var mimeReadLimit uint32 = 0

func SetMimeReadLimit(n uint32) {
	atomic.StoreUint32(&mimeReadLimit, n)
}

func DetectType(buf []byte) string {
	// Detect always returns valid MIME.
	mimetype.SetLimit(mimeReadLimit)
	return mimetype.Detect(buf).String()
}

func DetectExtension(buf []byte) string {
	mimetype.SetLimit(0)
	return mimetype.Detect(buf).Extension()
}

func DetectMime(stream io.Reader) (*mimetype.MIME, error) {
	mimetype.SetLimit(0)
	return mimetype.DetectReader(stream)
}

func IsAllowedContentType(contentType string, allowedContentType []string) bool {
	if len(allowedContentType) != 0 {
		for _, ct := range allowedContentType {
			if ct == contentType {
				return true
			}
		}
	}

	return false
}

type MediaTypeInfo interface {
	// MediaType returns the type of media for this info
	MediaType() MediaType

	// DirectoryName return a string which usually used to construct path for storing the media files
	DirectoryName() string

	// IsContentTypeAllowed returns true if the provided content type string is allowed for the media type
	IsContentTypeAllowed(contentType string) bool

	// MimeType returns mimetype.MIME wich is mimetype info of the document,
	// accept Reader as input, read whole file instead of partial byte of the Reader
	DetectReader(r io.Reader) (*mimetype.MIME, error)
}

var mediaTypeRegistry = map[MediaType]MediaTypeInfo{
	MediaType_IMAGE: &imageMediaTypeInfo{
		mediaType:     MediaType_IMAGE,
		directoryName: "images",
	},
	MediaType_FILE: &fileMediaTypeInfo{
		mediaType:     MediaType_FILE,
		directoryName: "files",
	},
}

func GetMediaTypeInfoByTypeName(mediaTypeName string) MediaTypeInfo {
	if mediaTypeName == "" {
		return nil
	}

	mediaTypeName = strings.ToUpper(mediaTypeName)
	if v, ok := MediaType_Value[mediaTypeName]; ok {
		mediaType := MediaType(v)
		return GetMediaTypeInfo(mediaType)
	}

	return nil
}

func GetMediaTypeInfo(mediaType MediaType) MediaTypeInfo {
	return mediaTypeRegistry[mediaType]
}

func ResizeImage(file io.Reader, contentType string, width, height uint) ([]byte, error) {
	var resizedImg image.Image
	buf := new(bytes.Buffer)
	switch contentType {
	case "image/png":
		imgSignature, err := png.Decode(file)
		if err != nil {
			return nil, err
		}
		resizedImg = resize.Resize(uint(width), uint(height), imgSignature, resize.Lanczos3)
		(&png.Encoder{CompressionLevel: png.NoCompression}).Encode(buf, resizedImg)
	case "image/jpg", "image/jpeg":
		imgSignature, err := jpeg.Decode(file)
		if err != nil {
			return nil, err
		}
		resizedImg = resize.Resize(uint(width), uint(height), imgSignature, resize.Lanczos3)
		jpeg.Encode(buf, resizedImg, nil)
	case "image/gif":
		imgSignature, err := gif.Decode(file)
		if err != nil {
			return nil, err
		}
		resizedImg = resize.Resize(uint(width), uint(height), imgSignature, resize.Lanczos3)
		gif.Encode(buf, resizedImg, nil)
	default:
		// without resizing
		return nil, fmt.Errorf("file is not an image or corrupted")
	}

	return buf.Bytes(), nil
}
