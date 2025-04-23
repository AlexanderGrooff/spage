package common

import (
	"fmt"
	"os"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LogFormat represents a supported logging format
type LogFormat string

// Available log formats
const (
	LogFormatPlain LogFormat = "plain"
	LogFormatJSON  LogFormat = "json"
	// YAML format removed as zap doesn't support it natively
)

var (
	logger        *zap.SugaredLogger
	atomicLevel   zap.AtomicLevel
	logOutputFile *os.File
	// ValidLogFormats contains all supported logging formats
	ValidLogFormats = []LogFormat{LogFormatPlain, LogFormatJSON}
)

func init() {
	// Initialize with a default development logger (human-readable, info level)
	// Configuration will be applied later when config is loaded
	atomicLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	encoderCfg := zap.NewDevelopmentEncoderConfig()
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atomicLevel,
	)
	baseLogger := zap.New(core)
	logger = baseLogger.Sugar()
	// Ensure logs are flushed on exit by calling logger.Sync() via defer
	// where the logger is created (e.g., in main or an init function higher up).
	// zap.RegisterFlushOnPanic() // This function does not exist in the zap package.
	// Attempt to sync logger on exit (best effort)
	// Consider using a more robust mechanism in your application shutdown logic if needed.
	// e.g., using os/signal to trap exit signals and call logger.Sync()
	// or using a dedicated exit hook library.
}

// Cleanup closes the log file if it was opened.
func Cleanup() {
	if logOutputFile != nil {
		_ = logger.Sync() // Ensure logs are flushed before closing
		_ = logOutputFile.Close()
		logOutputFile = nil
	}
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

// buildEncoder builds a zapcore.Encoder based on the logging configuration.
func buildEncoder(loggingCfg config.LoggingConfig) zapcore.Encoder {
	encoderCfg := zap.NewProductionEncoderConfig() // Start with production defaults

	if loggingCfg.Timestamps {
		encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder // Or zapcore.RFC3339TimeEncoder etc.
	} else {
		encoderCfg.TimeKey = "" // Disable timestamp key
	}
	encoderCfg.EncodeLevel = zapcore.CapitalLevelEncoder // Example: INFO, WARN

	switch LogFormat(loggingCfg.Format) {
	case LogFormatJSON:
		encoderCfg.EncodeLevel = zapcore.LowercaseLevelEncoder // JSON typically uses lowercase keys
		return zapcore.NewJSONEncoder(encoderCfg)
	case LogFormatPlain:
		fallthrough // Fallthrough to default console encoder
	default:
		// Use development config for console for more readable output
		devEncoderCfg := zap.NewDevelopmentEncoderConfig()
		if loggingCfg.Timestamps {
			devEncoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
		} else {
			devEncoderCfg.TimeKey = ""
		}
		devEncoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder // Colored level for console
		return zapcore.NewConsoleEncoder(devEncoderCfg)
	}
}

// buildCore builds a new zapcore.Core based on the configuration.
func buildCore(loggingCfg config.LoggingConfig, writer zapcore.WriteSyncer) zapcore.Core {
	encoder := buildEncoder(loggingCfg)
	return zapcore.NewCore(encoder, writer, atomicLevel)
}

// SetLogFormat reconfigures the logger based on the logging configuration.
func SetLogFormat(loggingCfg config.LoggingConfig) error {
	if !IsValidLogFormat(loggingCfg.Format) {
		// Default to plain if invalid format is provided, log a warning
		// Use fmt.Fprintf directly as the logger might not be configured yet
		fmt.Fprintf(os.Stderr, "WARN: Invalid log format %q. Valid formats are: %v. Defaulting to '%s'.\n", loggingCfg.Format, ValidLogFormats, LogFormatPlain)
		// LogWarnf("Invalid log format %q. Valid formats are: %v. Defaulting to '%s'.", loggingCfg.Format, ValidLogFormats, LogFormatPlain)
		loggingCfg.Format = string(LogFormatPlain)
	}

	var writer zapcore.WriteSyncer
	if logOutputFile != nil {
		writer = zapcore.Lock(logOutputFile)
	} else {
		writer = zapcore.Lock(os.Stdout)
	}

	core := buildCore(loggingCfg, writer)
	baseLogger := zap.New(core, zap.AddCallerSkip(1)) // Adjust caller skip if necessary
	logger = baseLogger.Sugar()

	// Re-apply log level after potential format change affecting level setting
	SetLogLevel(atomicLevel.Level().String()) // Use the current level string

	return nil
}

// SetLogLevel sets the logging level
func SetLogLevel(level string) {
	lvl, err := zapcore.ParseLevel(strings.ToLower(level))
	if err != nil {
		logger.Warnf("Invalid log level %q, using previous level: %s", level, atomicLevel.Level().String())
		// Keep the current level if parsing fails
		lvl = atomicLevel.Level()
	}
	atomicLevel.SetLevel(lvl)
}

// SetLogFile sets the output file for logging
func SetLogFile(path string) error {
	// Close existing file if open
	if logOutputFile != nil {
		_ = logger.Sync()
		_ = logOutputFile.Close()
		logOutputFile = nil
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		logger.Errorf("Failed to open log file %q: %v. Logging to stdout.", path, err)
		// Reset output to stdout if file opening fails
		currentFormat := LogFormatPlain // Assume plain or get from config if accessible
		// Try to get format from current logger if possible (more complex)
		// Rebuild core with stdout
		cfg := config.LoggingConfig{Format: string(currentFormat), Timestamps: true} // Assuming defaults
		core := buildCore(cfg, zapcore.Lock(os.Stdout))
		baseLogger := zap.New(core, zap.AddCallerSkip(1))
		logger = baseLogger.Sugar()
		return err // Return the error
	}
	logOutputFile = file

	// Rebuild core with the new file writer
	currentFormat := LogFormatPlain                                              // Assume plain or get from config if accessible
	cfg := config.LoggingConfig{Format: string(currentFormat), Timestamps: true} // Assuming defaults
	core := buildCore(cfg, zapcore.Lock(logOutputFile))
	baseLogger := zap.New(core, zap.AddCallerSkip(1))
	logger = baseLogger.Sugar()

	return nil
}

// SetExecutionID sets a execution ID field that will be included in all subsequent log entries
// This replaces the logger instance with a new one including the field.
func SetExecutionID(id string) {
	logger = logger.With(zap.String("execution_id", id))
}

// LogDebug logs a debug message using fmt.Sprintf style formatting.
func LogDebug(format string, args ...interface{}) {
	logger.Debugf(format, args...)
}

// LogInfo logs an info message using fmt.Sprintf style formatting.
func LogInfo(format string, args ...interface{}) {
	logger.Infof(format, args...)
}

// LogWarn logs a warning message using fmt.Sprintf style formatting.
func LogWarn(format string, args ...interface{}) {
	logger.Warnf(format, args...)
}

// LogError logs an error message using fmt.Sprintf style formatting.
func LogError(format string, args ...interface{}) {
	logger.Errorf(format, args...)
}

// DebugOutput logs a debug message using fmt.Sprintf style formatting.
// It respects the configured log level and formatter.
// This is now identical to LogDebug.
func DebugOutput(format string, args ...interface{}) {
	logger.Debugf(format, args...)
}
