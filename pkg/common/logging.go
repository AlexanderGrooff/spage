package common

import (
	"fmt"
	"os"

	"github.com/AlexanderGrooff/spage/pkg/config"
	"github.com/sirupsen/logrus"
)

// LogFormat represents a supported logging format
type LogFormat string

// Available log formats
const (
	LogFormatPlain LogFormat = "plain"
	LogFormatJSON  LogFormat = "json"
	LogFormatYAML  LogFormat = "yaml"
)

var (
	logger = logrus.New()
	// ValidLogFormats contains all supported logging formats
	ValidLogFormats = []LogFormat{LogFormatPlain, LogFormatJSON, LogFormatYAML}
)

func init() {
	// Default configuration will be overridden when config is loaded
	// Initialize with default config settings
	defaultLoggingCfg := config.LoggingConfig{
		Format:     string(LogFormatPlain),
		Timestamps: true, // Default timestamp setting
	}
	if err := SetLogFormat(defaultLoggingCfg); err != nil {
		// This should never happen with the default format
		fmt.Fprintf(os.Stderr, "Failed to set default log format: %v\n", err)
	}
	logger.SetOutput(os.Stdout)
	logger.SetLevel(logrus.InfoLevel) // Default level, overridden later
}

// IsValidLogFormat checks if the given format is supported
func IsValidLogFormat(format string) bool {
	for _, validFormat := range ValidLogFormats {
		if string(validFormat) == format {
			return true
		}
	}
	return false
}

// SetLogFormat sets the log formatter based on the logging configuration
func SetLogFormat(loggingCfg config.LoggingConfig) error {
	if !IsValidLogFormat(loggingCfg.Format) {
		return fmt.Errorf("invalid log format %q. Valid formats are: %v", loggingCfg.Format, ValidLogFormats)
	}

	timestampFormat := ""
	if loggingCfg.Timestamps {
		timestampFormat = "2006-01-02 15:04:05"
	}

	switch LogFormat(loggingCfg.Format) {
	case LogFormatJSON:
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat:  timestampFormat,
			DisableTimestamp: !loggingCfg.Timestamps,
		})
	case LogFormatYAML:
		// YAML format is achieved by using text formatter with custom sorting
		logger.SetFormatter(&logrus.TextFormatter{
			DisableColors:    true,
			TimestampFormat:  timestampFormat,
			FullTimestamp:    loggingCfg.Timestamps,
			DisableTimestamp: !loggingCfg.Timestamps,
			SortingFunc: func(keys []string) {
				// Sort keys to ensure consistent YAML-like output
				for i := 0; i < len(keys); i++ {
					for j := i + 1; j < len(keys); j++ {
						if keys[i] > keys[j] {
							keys[i], keys[j] = keys[j], keys[i]
						}
					}
				}
			},
		})
	case LogFormatPlain:
		logger.SetFormatter(&logrus.TextFormatter{
			DisableColors:    false,
			TimestampFormat:  timestampFormat,
			FullTimestamp:    loggingCfg.Timestamps,
			DisableTimestamp: !loggingCfg.Timestamps,
		})
	}
	return nil
}

// SetLogLevel sets the logging level
func SetLogLevel(level string) {
	lvl, err := logrus.ParseLevel(level)
	if err != nil {
		logger.Warnf("Invalid log level %q, defaulting to info", level)
		lvl = logrus.InfoLevel
	}
	logger.SetLevel(lvl)
}

// SetLogFile sets the output file for logging
func SetLogFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}
	logger.SetOutput(file)
	return nil
}

// SetOutputFormat is deprecated, use SetLogFormat instead
// Note: This function cannot honor the timestamp setting as it doesn't receive the full config.
func SetOutputFormat(format string) {
	// Create a temporary logging config with default timestamp setting
	tempLoggingCfg := config.LoggingConfig{
		Format:     format,
		Timestamps: true, // Cannot know the actual config value here
	}
	if err := SetLogFormat(tempLoggingCfg); err != nil {
		logger.Warnf("Failed to set output format using deprecated function: %v", err)
	}
}

// SetExecutionID sets a execution ID field that will be included in all subsequent log entries
func SetExecutionID(id string) {
	logger.AddHook(&executionIDHook{id: id})
}

// executionIDHook adds execution ID to all log entries
type executionIDHook struct {
	id string
}

func (h *executionIDHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *executionIDHook) Fire(entry *logrus.Entry) error {
	entry.Data["execution_id"] = h.id
	return nil
}

// LogDebug logs a debug message
func LogDebug(msg string, fields map[string]interface{}) {
	logger.WithFields(fields).Debug(msg)
}

// LogInfo logs an info message
func LogInfo(msg string, fields map[string]interface{}) {
	logger.WithFields(fields).Info(msg)
}

// LogWarn logs a warning message
func LogWarn(msg string, fields map[string]interface{}) {
	logger.WithFields(fields).Warn(msg)
}

// LogError logs an error message
func LogError(msg string, fields map[string]interface{}) {
	logger.WithFields(fields).Error(msg)
}

// DebugOutput logs a debug message using fmt.Sprintf style formatting.
// It respects the configured log level and formatter (including plain text, JSON, YAML).
func DebugOutput(format string, args ...interface{}) {
	logger.Debugf(format, args...)
}
