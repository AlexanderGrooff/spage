package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration settings
type Config struct {
	Logging             LoggingConfig             `mapstructure:"logging"`
	ExecutionMode       string                    `mapstructure:"execution_mode"`
	Executor            string                    `mapstructure:"executor"` // "local" or "temporal"
	Temporal            TemporalConfig            `mapstructure:"temporal"`
	API                 APIConfig                 `mapstructure:"api"`
	Daemon              DaemonConfig              `mapstructure:"daemon"`
	Revert              bool                      `mapstructure:"revert"`
	Tags                TagsConfig                `mapstructure:"tags"`
	Facts               map[string]interface{}    `mapstructure:"facts"`
	HostKeyChecking     bool                      `mapstructure:"host_key_checking"`
	RolesPath           string                    `mapstructure:"roles_path"` // Colon-delimited paths to search for roles
	Inventory           string                    `mapstructure:"inventory"`  // Colon-delimited paths to search for inventory files
	PrivilegeEscalation PrivilegeEscalationConfig `mapstructure:"privilege_escalation"`

	// API bearer token for CLI auth against spage-api
	ApiToken string `mapstructure:"api_token"`

	// Host limit pattern for execution
	Limit string `mapstructure:"limit"`

	// Internal fields for daemon reporting (not serialized)
	daemonReporting interface{}
}

// APIConfig holds API-related configuration
type APIConfig struct {
	// Base URL for the Spage API HTTP server (used for bundles)
	HTTPBase string `mapstructure:"http_base"`
}

// LoggingConfig holds logging-related configuration
type LoggingConfig struct {
	Level      string `mapstructure:"level"`
	File       string `mapstructure:"file"`
	Format     string `mapstructure:"format"`
	Timestamps bool   `mapstructure:"timestamps"`
}

// TemporalConfig holds Temporal-specific configuration
type TemporalConfig struct {
	Address          string `mapstructure:"address"`
	TaskQueue        string `mapstructure:"task_queue"`
	WorkflowIDPrefix string `mapstructure:"workflow_id_prefix"`
	Trigger          bool   `mapstructure:"trigger"`
}

// TagsConfig holds tag filtering configuration
type TagsConfig struct {
	Tags     []string `mapstructure:"tags"`      // Only run tasks with these tags
	SkipTags []string `mapstructure:"skip_tags"` // Skip tasks with these tags
}

// PrivilegeEscalationConfig holds privilege escalation configuration
type PrivilegeEscalationConfig struct {
	UseInteractive bool   `mapstructure:"use_interactive"` // Use -Su (interactive) vs -u (non-interactive)
	BecomeFlags    string `mapstructure:"become_flags"`    // Additional flags to pass to become
}

// DaemonConfig holds daemon communication configuration
type DaemonConfig struct {
	Enabled  bool          `mapstructure:"enabled"`
	Endpoint string        `mapstructure:"endpoint"`
	PlayID   string        `mapstructure:"play_id"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

// ValidOutputFormats contains the list of supported output formats
var ValidOutputFormats = []string{"json", "yaml", "plain"}

// ValidExecutionModes contains the list of supported execution modes
var ValidExecutionModes = []string{"parallel", "sequential"}

var ValidExecutors = []string{"local", "temporal"}

// SetDaemonReporting sets the daemon reporting instance
func (c *Config) SetDaemonReporting(reporting interface{}) {
	c.daemonReporting = reporting
}

// GetDaemonReporting gets the daemon reporting instance
func (c *Config) GetDaemonReporting() interface{} {
	return c.daemonReporting
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

	// Validate executor
	if !isValidExecutor(config.Executor) {
		return nil, fmt.Errorf("invalid executor %q. Valid executors are: %s",
			config.Executor, strings.Join(ValidExecutors, ", "))
	}

	return &config, nil
}

func setDefaults(v *viper.Viper) {
	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.file", "")
	v.SetDefault("logging.format", "plain")
	v.SetDefault("execution_mode", "sequential")
	v.SetDefault("executor", "local")
	v.SetDefault("logging.timestamps", true)
	v.SetDefault("revert", false)

	// API defaults
	v.SetDefault("api.http_base", "http://localhost:1323")
	// CLI/API auth token default (unset)
	v.SetDefault("api_token", "")

	// Host limit pattern default (unset)
	v.SetDefault("limit", "")

	// Temporal defaults
	v.SetDefault("temporal.address", "") // Default to empty, SDK will use localhost:7233 or TEMPORAL_GRPC_ENDPOINT
	v.SetDefault("temporal.task_queue", "SPAGE_DEFAULT_TASK_QUEUE")
	v.SetDefault("temporal.workflow_id_prefix", "spage-workflow")
	v.SetDefault("temporal.trigger", false) // Default to false

	// Tags defaults
	v.SetDefault("tags.tags", []string{})      // Default to empty (run all tasks)
	v.SetDefault("tags.skip_tags", []string{}) // Default to empty (skip no tasks)

	// Facts default
	v.SetDefault("facts", map[string]interface{}{})

	// HostKeyChecking default
	v.SetDefault("host_key_checking", true)

	// RolesPath default
	v.SetDefault("roles_path", "") // Default to empty, SDK will use default roles

	// Inventory default
	v.SetDefault("inventory", "") // Default to empty, SDK will use default inventory

	// PrivilegeEscalation defaults
	v.SetDefault("privilege_escalation.use_interactive", false) // Default to non-interactive (-u) for SSH sessions
	v.SetDefault("privilege_escalation.become_flags", "")

	// Daemon defaults
	v.SetDefault("daemon.enabled", false) // Default to disabled
	v.SetDefault("daemon.endpoint", "localhost:9091")
	v.SetDefault("daemon.play_id", "")
	v.SetDefault("daemon.timeout", 3*time.Second) // Default to 3 seconds
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

func isValidExecutor(executor string) bool {
	for _, validExecutor := range ValidExecutors {
		if executor == validExecutor {
			return true
		}
	}
	return false
}
