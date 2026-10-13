package logger

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/rs/zerolog"
	"github.com/tomasen/realip"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/dirty-go/foundation/errors"
)

type Fields map[string]any

type (
	Logger = zerolog.Logger
)

type PkgLogger struct {
	Logger
}

const EnvPrefixDefault = "LOG_"

func newRollingFile(config Config) io.Writer {
	filename := config.Filename
	if filename == "" {
		filename = "service.log"
	}
	if err := os.MkdirAll(config.Directory, 0744); err != nil {
		return nil
	}
	logFilename := path.Join(config.Directory, filename)

	return &lumberjack.Logger{
		Filename:   logFilename,
		MaxBackups: config.MaxBackups, // files
		MaxSize:    config.MaxSize,    // megabytes
		MaxAge:     config.MaxAge,     // days
	}
}

func newLogger() Logger {
	if logPretty := os.Getenv(EnvPrefixDefault + "PRETTY"); logPretty == "true" {
		return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	}

	return zerolog.New(os.Stderr)
}

// loadConfigFromEnv returns the skeleton config overridden by the variables
// that are set.
func loadConfigFromEnv(opts env.Options) (Config, error) {
	cfg := configSkeletonPtr()
	err := env.ParseWithOptions(&cfg, opts)
	return cfg, err
}

// describeConfigError names the fields that failed to load without echoing
// their values.
func describeConfigError(err error) string {
	var aggErr env.AggregateError
	if !errors.As(err, &aggErr) {
		return "invalid configuration"
	}
	fields := make([]string, 0, len(aggErr.Errors))
	for _, e := range aggErr.Errors {
		var parseErr env.ParseError
		if errors.As(e, &parseErr) {
			fields = append(fields, "invalid "+parseErr.Name)
			continue
		}
		var notSetErr env.VarIsNotSetError
		if errors.As(e, &notSetErr) {
			fields = append(fields, "missing "+notSetErr.Key)
			continue
		}
		fields = append(fields, "invalid configuration")
	}
	return strings.Join(fields, ", ")
}

func newLoggerByEnv() Logger {
	logger := newLogger()

	if logLevelStr := os.Getenv(EnvPrefixDefault + "LEVEL"); logLevelStr != "" {
		logLevel, err := zerolog.ParseLevel(logLevelStr)
		if err != nil {
			panic(err)
		}
		logger = logger.Level(logLevel)
	}

	cfg, err := loadConfigFromEnv(env.Options{Prefix: EnvPrefixDefault})
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger: loading %s* config from environment failed (%s); "+
			"falling back to default JSON stderr logger, console and file writers not configured\n",
			EnvPrefixDefault, describeConfigError(err))
	} else {
		writers := []io.Writer{
			zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
		}

		if cfg.FileLogging {
			logFile := newRollingFile(cfg)
			writers = append(writers, logFile)
		}

		mw := zerolog.MultiLevelWriter(writers...)
		logger = logger.Output(mw)
	}

	logCtx := logger.With()
	if os.Getenv("AWS_EXECUTION_ENV") != "" {
		// 	TODO: if we are detecting an environment which already providing
		// 	timestamp, we should disable the timestamp by default
	} else {
		logCtx = logCtx.Timestamp()
	}

	return logCtx.Logger()
}
func NewPkgLogger() PkgLogger {
	logCtx := newLoggerByEnv().With().CallerWithSkipFrameCount(2)

	return PkgLogger{logCtx.Logger()}
}

func (logger PkgLogger) WithRequest(req *http.Request) *Logger {
	if req == nil {
		return &logger.Logger
	}

	var urlStr string
	if req.URL != nil {
		urlStr = req.URL.String()
	}
	remoteAddr := realip.FromRequest(req)
	if remoteAddr == "" {
		remoteAddr = req.RemoteAddr
	}
	l := logger.With().
		Str("method", req.Method).
		Str("url", urlStr).
		Str("remote_ip", remoteAddr).
		Str("user_agent", req.UserAgent()).
		Logger()

	return &l
}
