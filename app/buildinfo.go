package app

type BuildInfo struct {
	RevisionID string
	Timestamp  string
}

var (
	buildRevisionID string = "unknown"
	buildTimestamp  string = "unknown"
)

func SetBuildInfo(
	revisionID string,
	timestamp string,
) {
	buildRevisionID = revisionID
	buildTimestamp = timestamp
}

func GetBuildInfo() BuildInfo {
	return BuildInfo{
		RevisionID: buildRevisionID,
		Timestamp:  buildTimestamp,
	}
}
