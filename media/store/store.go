package store

import (
	"bytes"
	"encoding/hex"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/timemore/foundation/errors"
	"github.com/timemore/foundation/media"
	"golang.org/x/crypto/blake2b"
)

type Store struct {
	config        Config
	serviceClient Service
}

type Object interface {
	io.Reader
	io.Seeker
	io.WriterAt
}

func New(config Config) (*Store, error) {
	if len(config.Modules) == 0 {
		return nil, errors.ArgMsg("config.Modules", "empty")
	}
	if config.StoreService == "" {
		return nil, errors.ArgMsg("config.StoreService", "empty")
	}

	modsAvailable := ModuleNames()
	modCfg := config.Modules[config.StoreService]
	if modCfg == nil {
		return nil, errors.ArgMsg("config.StoreService", config.StoreService+" not configured, availbale StoreService name : "+strings.Join(modsAvailable, ","))
	}
	serviceClient, err := NewServiceClient(config.StoreService, modCfg)
	if err != nil {
		return nil, errors.ArgWrap("config.StoreService", config.StoreService+" initialization failed", err)
	}

	return &Store{
		config:        config,
		serviceClient: serviceClient,
	}, nil
}

func (mediaStore *Store) Upload(
	mediaName string,
	contentSource io.Reader,
	mediaType media.MediaType,
) (uploadInfo *UploadInfo, err error) {
	uploadInfo, err = mediaStore.serviceClient.PutObject(mediaName, contentSource)
	if err != nil {
		return nil, errors.Wrap("putting object", err)
	}
	tNow := time.Now().UTC()
	if uploadInfo.LastModified.IsZero() {
		uploadInfo.LastModified = tNow
	}

	return uploadInfo, nil
}

func (mediaStore *Store) GetPublicURL(sourceKey string) (publicURL string, err error) {
	return mediaStore.serviceClient.GetPublicObject(sourceKey)
}

func (mediaStore *Store) Download(sourceKey string) (buffer *bytes.Buffer, err error) {
	return mediaStore.serviceClient.GetObject(sourceKey)
}

const nameGenHashLength = 16

const nameGenKeyDefault = "N0k3y"

// GenerateName is used to generate a name, for file or other identifier
// based on the content. It utilized hash so the result could be used to
// prevent duplicates when storing the media object
func (mediaStore *Store) GenerateName(stream io.Reader) string {
	keyBytes := []byte(mediaStore.config.NameGenerationKey)
	if len(keyBytes) == 0 {
		keyBytes = []byte(nameGenKeyDefault)
	}
	hasher, err := blake2b.New(nameGenHashLength, keyBytes)
	if err != nil {
		panic(err)
	}
	buf := make([]byte, 1*1024*1024)
	dataSize := 0
	for {
		n, err := stream.Read(buf)
		if err != nil {
			if err != io.EOF {
				panic(err)
			}
		}
		dataSize += n
		hasher.Write(buf)
		if err == io.EOF || n == 0 {
			break
		}
	}

	hashBytes := hasher.Sum(nil)

	encodedHash := hex.EncodeToString(hashBytes) + "K" + hex.EncodeToString(keyBytes[:4]) + "N" + strconv.FormatInt(int64(dataSize), 16)

	return encodedHash
}

func ContentTypeInList(contentType string, contentTypeList []string) bool {
	for _, ct := range contentTypeList {
		if ct == contentType {
			return true
		}
	}
	return false
}

type UploadInfo struct {
	Bucket       string
	ETag         string
	Key          string
	LastModified time.Time
	Location     string
	Output       *bytes.Buffer
	Size         int
	UploadID     string
}
