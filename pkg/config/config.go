package config

import (
	"fmt"
	"strings"

	// "github.com/AlexanderGrooff/spage/pkg" // Removed to break import cycle
	"github.com/spf13/viper"
)

// Config holds all configuration settings
type Config struct {
	Logging       LoggingConfig `mapstructure:"logging"`
	ExecutionMode string        `mapstructure:"execution_mode"`
}

// LoggingConfig holds logging-related configuration
type LoggingConfig struct {
	Level      string `mapstructure:"level"`
	File       string `mapstructure:"file"`
	Format     string `mapstructure:"format"`
	Timestamps bool   `mapstructure:"timestamps"`
}

// ValidOutputFormats contains the list of supported output formats
var ValidOutputFormats = []string{"json", "yaml", "plain"}

// ValidExecutionModes contains the list of supported execution modes
var ValidExecutionModes = []string{"parallel", "sequential"}

// Load loads configuration from files and environment variables
func Load(configPaths ...string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Read config files
	for _, path := range configPaths {
		v.SetConfigFile(path)
		if err := v.MergeInConfig(); err != nil {
			return nil, fmt.Errorf("error reading config file %s: %w", path, err)
		}
	}

	// Environment variables
	v.SetEnvPrefix("SPAGE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	// Validate output format
	if !isValidOutputFormat(config.Logging.Format) {
		return nil, fmt.Errorf("invalid output format %q. Valid formats are: %s",
			config.Logging.Format, strings.Join(ValidOutputFormats, ", "))
	}

	// Validate execution mode
	if !isValidExecutionMode(config.ExecutionMode) {
		return nil, fmt.Errorf("invalid execution mode %q. Valid modes are: %s",
			config.ExecutionMode, strings.Join(ValidExecutionModes, ", "))
	}

	return &config, nil
}

func setDefaults(v *viper.Viper) {
	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.file", "")
	v.SetDefault("logging.format", "plain")
	v.SetDefault("execution_mode", "parallel")
	v.SetDefault("logging.timestamps", true)
}

// isValidOutputFormat checks if the given format is supported
func isValidOutputFormat(format string) bool {
	for _, validFormat := range ValidOutputFormats {
		if format == validFormat {
			return true
		}
	}
	return false
}

// isValidExecutionMode checks if the given execution mode is supported
func isValidExecutionMode(mode string) bool {
	for _, validMode := range ValidExecutionModes {
		if mode == validMode {
			return true
		}
	}
	return false
}
