package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config holds all configuration settings
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Logging  LoggingConfig  `mapstructure:"logging"`
	Security SecurityConfig `mapstructure:"security"`
}

// ServerConfig holds server-related configuration
type ServerConfig struct {
	Port         string `mapstructure:"port"`
	MetricsPort  string `mapstructure:"metrics_port"`
	ReadTimeout  int    `mapstructure:"read_timeout"`
	WriteTimeout int    `mapstructure:"write_timeout"`
}

// LoggingConfig holds logging-related configuration
type LoggingConfig struct {
	Level string `mapstructure:"level"`
	File  string `mapstructure:"file"`
}

// SecurityConfig holds security-related configuration
type SecurityConfig struct {
	EnableTLS bool   `mapstructure:"enable_tls"`
	CertFile  string `mapstructure:"cert_file"`
	KeyFile   string `mapstructure:"key_file"`
}

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

	return &config, nil
}

func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.port", "8080")
	v.SetDefault("server.metrics_port", "9090")
	v.SetDefault("server.read_timeout", 30)
	v.SetDefault("server.write_timeout", 30)

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.file", "")

	// Security defaults
	v.SetDefault("security.enable_tls", false)
	v.SetDefault("security.cert_file", "")
	v.SetDefault("security.key_file", "")
}
