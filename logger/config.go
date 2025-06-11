package logger

type Config struct {
	// Enable console logging
	ConsoleLog bool
	// EncodeLogsAsJson makes the log framework log JSON
	EncodeLogsAsJson bool
	// FileLoggingEnabled makes the framework log to a file
	// the fields below can be skipped if this value is false!
	FileLogging bool
	// Directory to log to to when file logging is enabled
	Directory string
	// Filename is the name of the logfile which will be placed inside the directory
	Filename string
	// MaxSize the max size in MB of the logfile before it's rolled
	MaxSize int
	// MaxBackups the max number of rolled files to keep
	MaxBackups int
	// MaxAge the max age in days to keep a logfile
	MaxAge int
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
