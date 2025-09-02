package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration settings
type Config struct {
	Logging         LoggingConfig          `mapstructure:"logging"`
	ExecutionMode   string                 `mapstructure:"execution_mode"`
	Executor        string                 `mapstructure:"executor"` // "local" or "temporal"
	Temporal        TemporalConfig         `mapstructure:"temporal"`
	API             APIConfig              `mapstructure:"api"`
	Daemon          DaemonConfig           `mapstructure:"daemon"`
	Revert          bool                   `mapstructure:"revert"`
	Tags            TagsConfig             `mapstructure:"tags"`
	Facts           map[string]interface{} `mapstructure:"facts"`
	HostKeyChecking bool                   `mapstructure:"host_key_checking"`
	RolesPath       string                 `mapstructure:"roles_path"` // Colon-delimited paths to search for roles
	Inventory       string                 `mapstructure:"inventory"`  // Colon-delimited paths to search for inventory files
	// Inventory plugins configuration (Ansible-like behaviour)
	// Colon-delimited paths to search for inventory plugins (e.g. "./plugins/inventory:/usr/share/ansible/plugins/inventory")
	InventoryPlugins string `mapstructure:"inventory_plugins"`
	// List of enabled plugin names/patterns (e.g. ["host_list"])
	EnablePlugins       []string                  `mapstructure:"enable_plugins"`
	PrivilegeEscalation PrivilegeEscalationConfig `mapstructure:"privilege_escalation"`
	SSH                 SSHConfig                 `mapstructure:"ssh"`

	// Connection type override (e.g., "local" to run tasks locally)
	Connection string `mapstructure:"connection"`

	// API bearer token for CLI auth against spage-api
	ApiToken string `mapstructure:"api_token"`

	// Host limit pattern for execution
	Limit string `mapstructure:"limit"`

	// Ansible vault password for decrypting encrypted variables in inventory
	AnsibleVaultPassword string `mapstructure:"ansible_vault_password"`

	// Ansible vault configuration
	AnsibleVault AnsibleVaultConfig `mapstructure:"ansible_vault"`

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

// SSHConfig holds SSH-related configuration
type SSHConfig struct {
	JumpHost string `mapstructure:"jump_host"` // SSH jump host (ProxyJump equivalent), use "none" to disable
	JumpUser string `mapstructure:"jump_user"` // Username for jump host
	JumpPort int    `mapstructure:"jump_port"` // Port for jump host (default 22)

	// Authentication configuration
	Auth SSHAuthConfig `mapstructure:"auth"`

	// Advanced options (ssh_config style)
	Options map[string]string `mapstructure:"options"`
}

// AnsibleVaultConfig holds Ansible vault configuration
type AnsibleVaultConfig struct {
	// Vault password file path
	PasswordFile string `mapstructure:"password_file"`

	// Vault identity list (colon-delimited paths to vault identity files)
	Identity string `mapstructure:"identity"`

	// Vault password prompt
	PasswordPrompt string `mapstructure:"password_prompt"`

	// Ask for vault password
	AskVaultPass bool `mapstructure:"ask_vault_pass"`

	// Ask for vault password when doing a diff
	AskVaultPassOnDiff bool `mapstructure:"ask_vault_pass_on_diff"`

	// Vault timeout in seconds
	Timeout int `mapstructure:"timeout"`

	// Vault retry count
	RetryCount int `mapstructure:"retry_count"`

	// Vault retry delay in seconds
	RetryDelay int `mapstructure:"retry_delay"`
}

// SSHAuthConfig holds SSH authentication method configuration
type SSHAuthConfig struct {
	Methods         []string `mapstructure:"methods"`          // Ordered list of auth methods to try: "publickey", "password", "keyboard-interactive", "gssapi-with-mic", "none"
	PublicKeys      []string `mapstructure:"public_keys"`      // Paths to specific public key files (if empty, uses SSH agent + default keys)
	PasswordAuth    bool     `mapstructure:"password"`         // Enable password authentication
	KeyboardAuth    bool     `mapstructure:"keyboard"`         // Enable keyboard-interactive authentication
	NoneAuth        bool     `mapstructure:"none"`             // Enable "none" authentication method
	PreferredAuth   string   `mapstructure:"preferred"`        // Preferred authentication method
	IdentitiesOnly  bool     `mapstructure:"identities_only"`  // Only use explicitly configured identities
	PasswordPrompt  string   `mapstructure:"password_prompt"`  // Custom password prompt
	AgentForwarding bool     `mapstructure:"agent_forwarding"` // Enable SSH agent forwarding
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

// ResolveVaultPassword resolves the vault password from various sources
// Priority: 1) Direct password, 2) Password file, 3) Identity files, 4) Interactive prompt
func (c *Config) ResolveVaultPassword() (string, error) {
	// If password is already set, use it
	if c.AnsibleVaultPassword != "" {
		return c.AnsibleVaultPassword, nil
	}

	// Try password file
	if c.AnsibleVault.PasswordFile != "" {
		password, err := os.ReadFile(c.AnsibleVault.PasswordFile)
		if err != nil {
			return "", fmt.Errorf("failed to read vault password file %s: %w", c.AnsibleVault.PasswordFile, err)
		}
		// Trim whitespace and newlines
		return strings.TrimSpace(string(password)), nil
	}

	// Try identity files
	if c.AnsibleVault.Identity != "" {
		identities := strings.Split(c.AnsibleVault.Identity, ":")
		for _, identityPath := range identities {
			identityPath = strings.TrimSpace(identityPath)
			if identityPath == "" {
				continue
			}

			// Try to read the identity file
			if data, err := os.ReadFile(identityPath); err == nil {
				// For now, assume identity files contain the password directly
				// In a full implementation, this would handle different identity file formats
				return strings.TrimSpace(string(data)), nil
			}
		}
	}

	// Try interactive prompt if enabled
	if c.AnsibleVault.AskVaultPass {
		// For now, return an error indicating interactive mode is not implemented
		// In a full implementation, this would prompt the user for input
		return "", fmt.Errorf("interactive vault password prompt not implemented yet")
	}

	// No password found
	return "", nil
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

	// Resolve vault password
	password, err := config.ResolveVaultPassword()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve vault password: %w", err)
	}
	config.AnsibleVaultPassword = password

	return &config, nil
}

func setDefaults(v *viper.Viper) {
	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.file", "")
	v.SetDefault("logging.format", "plain")
	v.SetDefault("execution_mode", "sequential")
	v.SetDefault("executor", "local")
	// Connection default: empty means use default remote execution (ssh). Set to "local" to force local.
	v.SetDefault("connection", "")
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
	// Inventory plugins defaults
	v.SetDefault("inventory_plugins", "")
	v.SetDefault("enable_plugins", []string{})

	// PrivilegeEscalation defaults
	v.SetDefault("privilege_escalation.use_interactive", false) // Default to non-interactive (-u) for SSH sessions
	v.SetDefault("privilege_escalation.become_flags", "")

	// SSH defaults
	v.SetDefault("ssh.use_password_fallback", false) // Default to disabled for security
	v.SetDefault("ssh.jump_host", "")                // Default to no jump host
	v.SetDefault("ssh.jump_user", "")                // Default to empty (use same user as main connection)
	v.SetDefault("ssh.jump_port", 22)                // Default SSH port

	// SSH Authentication defaults
	v.SetDefault("ssh.auth.methods", []string{"publickey", "password"})
	v.SetDefault("ssh.auth.public_keys", []string{})
	v.SetDefault("ssh.auth.password", true)
	v.SetDefault("ssh.auth.keyboard", false)
	v.SetDefault("ssh.auth.gssapi", false)
	v.SetDefault("ssh.auth.none", false)
	v.SetDefault("ssh.auth.preferred", "publickey")
	v.SetDefault("ssh.auth.identities_only", false)
	v.SetDefault("ssh.auth.password_prompt", "")
	v.SetDefault("ssh.auth.agent_forwarding", false)

	// AnsibleVault defaults
	v.SetDefault("ansible_vault.password_file", "")
	v.SetDefault("ansible_vault.identity", "")
	v.SetDefault("ansible_vault.password_prompt", "")
	v.SetDefault("ansible_vault.ask_vault_pass", false)
	v.SetDefault("ansible_vault.ask_vault_pass_on_diff", false)
	v.SetDefault("ansible_vault.timeout", 30)
	v.SetDefault("ansible_vault.retry_count", 3)
	v.SetDefault("ansible_vault.retry_delay", 5)

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
