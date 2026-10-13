package logger

// Config is loaded from LOG_-prefixed environment variables; the env tag
// names keep the names previously derived by rez-go/stev. Unset variables
// keep the skeleton defaults, and an empty bool variable counts as unset.
type Config struct {
	// Enable console logging
	ConsoleLog bool `env:"CONSOLE_LOG"`
	// EncodeLogsAsJson makes the log framework log JSON
	EncodeLogsAsJson bool `env:"ENCODE_LOGS_AS_JSON"` //nolint:revive // exported name kept for API compatibility
	// FileLoggingEnabled makes the framework log to a file
	// the fields below can be skipped if this value is false!
	FileLogging bool `env:"FILE_LOGGING"`
	// Directory to log to to when file logging is enabled
	Directory string `env:"DIRECTORY"`
	// Filename is the name of the logfile which will be placed inside the directory
	Filename string `env:"FILENAME"`
	// MaxSize the max size in MB of the logfile before it's rolled
	MaxSize int `env:"MAX_SIZE"`
	// MaxBackups the max number of rolled files to keep
	MaxBackups int `env:"MAX_BACKUPS"`
	// MaxAge the max age in days to keep a logfile
	MaxAge int `env:"MAX_AGE"`
}

func configSkeletonPtr() Config {
	return Config{
		ConsoleLog:       true,
		EncodeLogsAsJson: true,
		FileLogging:      false,
		Directory:        "/var/log",
		Filename:         "",
		MaxSize:          2,
		MaxBackups:       4,
		MaxAge:           30,
	}
}
