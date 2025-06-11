package media

type MediaType int32

const (
	// The Default value
	MediaType_MEDIA_TYPE_UNSPECIFIED MediaType = 0
	// MediaType_MEDIA_TYPE_UNKNOWN Type is unknown. This is usually used when a process was unable to determine the type.
	MediaType_MEDIA_TYPE_UNKNOWN MediaType = 1
	MediaType_TEXT               MediaType = 4
	MediaType_IMAGE              MediaType = 5
	MediaType_AUDIO              MediaType = 6
	MediaType_VIDEO              MediaType = 7
	MediaType_FILE               MediaType = 8
)

var MediaType_name = map[int32]string{
	0: "MEDIA_TYPE_UNSPECIFIED",
	1: "MEDIA_TYPE_UNKNOWN",
	4: "TEXT",
	5: "IMAGE",
	6: "AUDIO",
	7: "VIDEO",
	8: "FILE",
}

var MediaType_Value = map[string]int32{
	"MEDIA_TYPE_UNSPECIFIED": 0,
	"MEDIA_TYPE_UNKNOWN":     1,
	"TEXT":                   4,
	"IMAGE":                  5,
	"AUDIO":                  6,
	"VIDEO":                  7,
	"FILE":                   8,
}

func (x MediaType) String() string {
	return MediaType_name[int32(x)]
}
