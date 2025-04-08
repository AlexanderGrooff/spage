package pkg

import (
	"os"

	"github.com/sirupsen/logrus"
)

var logger = logrus.New()

func init() {
	// Set default configuration
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetOutput(os.Stdout)
	logger.SetLevel(logrus.InfoLevel)
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

// DebugOutput is maintained for backward compatibility
// It should be removed in favor of structured logging in the future
func DebugOutput(format string, args ...interface{}) {
	logger.Debugf(format, args...)
}
