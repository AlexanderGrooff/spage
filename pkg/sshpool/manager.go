package sshpool

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
	desopssshpool "github.com/desops/sshpool"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"
)

// authMethodCache stores detected authentication methods for hosts
type authMethodCache struct {
	methods []string
	expires time.Time
}

// Manager manages SSH connection pools for multiple hosts
type Manager struct {
	pools map[string]*desopssshpool.Pool
	mu    sync.RWMutex
	cfg   *config.Config
	// Cache for SSH authentication methods to avoid repeated server detection
	authMethodCache map[string]*authMethodCache
	authCacheMu     sync.RWMutex
}

// NewManager creates a new SSH pool manager
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		pools:           make(map[string]*desopssshpool.Pool),
		cfg:             cfg,
		authMethodCache: make(map[string]*authMethodCache),
	}
}

// GetPool returns or creates an SSH pool for the given host
func (m *Manager) GetPool(host string, hostVars map[string]interface{}) (*desopssshpool.Pool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if pool, exists := m.pools[host]; exists {
		return pool, nil
	}

	// Create new pool for this host
	pool, err := m.createPool(host, hostVars)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH pool for host %s: %w", host, err)
	}

	m.pools[host] = pool
	return pool, nil
}

// createPool creates a new SSH pool for a specific host
func (m *Manager) createPool(host string, hostVars map[string]interface{}) (*desopssshpool.Pool, error) {
	// Try to create SSH client config with existing methods first
	sshConfig, err := m.createSSHConfig(host, hostVars)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH config for host %s: %w", host, err)
	}

	// Create pool configuration with reasonable defaults
	poolConfig := &desopssshpool.PoolConfig{
		Debug:             true, // Enable debug to see SSH negotiation
		MaxSessions:       10,   // Default SSH server limit
		MaxConnections:    5,    // Conservative default
		SessionCloseDelay: 20 * time.Millisecond,
	}

	// Handle jump host configuration by modifying the target host address
	var pool *desopssshpool.Pool
	if m.cfg != nil && m.cfg.SSH.JumpHost != "" && m.cfg.SSH.JumpHost != "none" {
		common.LogInfo("Configuring SSH connection through jump host", map[string]interface{}{
			"host":      host,
			"jump_host": m.cfg.SSH.JumpHost,
			"jump_user": m.cfg.SSH.JumpUser,
			"jump_port": m.cfg.SSH.JumpPort,
		})

		// Create pool with jump host dialer
		pool = m.createPoolWithJumpHost(host, sshConfig, poolConfig)
	} else {
		if m.cfg != nil && m.cfg.SSH.JumpHost == "none" {
			common.LogInfo("Jump host explicitly disabled with 'none'", map[string]interface{}{
				"host": host,
			})
		}
		// Create standard pool
		pool = desopssshpool.New(sshConfig, poolConfig)
	}

	// The pool is created with authentication methods (including password if enabled)
	// No need to test the connection here as it will be tested when actually used
	// This avoids premature connection attempts that might not trigger password prompts properly

	return pool, nil
}

// createPoolWithJumpHost creates an SSH pool that connects through a jump host
// Note: This is a placeholder implementation. Full jump host support would require
// either extending the sshpool library or implementing custom connection management.
func (m *Manager) createPoolWithJumpHost(targetHost string, targetConfig *ssh.ClientConfig, poolConfig *desopssshpool.PoolConfig) *desopssshpool.Pool {
	jumpUser := m.cfg.SSH.JumpUser
	if jumpUser == "" {
		jumpUser = targetConfig.User
	}

	jumpPort := m.cfg.SSH.JumpPort
	if jumpPort == 0 {
		jumpPort = 22
	}

	common.LogInfo("Jump host configuration detected but not fully implemented yet", map[string]interface{}{
		"jump_host":   m.cfg.SSH.JumpHost,
		"jump_user":   jumpUser,
		"jump_port":   jumpPort,
		"target_host": targetHost,
		"note":        "Currently creating direct connection - jump host functionality requires additional implementation",
	})

	// For now, create a direct connection pool
	// TODO: Implement proper ProxyJump functionality
	return desopssshpool.New(targetConfig, poolConfig)
}

// createSSHConfig creates SSH client configuration for a host using the new generic configuration
func (m *Manager) createSSHConfig(host string, hostVars map[string]interface{}) (*ssh.ClientConfig, error) {
	currentUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user for SSH connection to %s: %w", host, err)
	}

	// Build authentication methods based on configuration
	authMethods, err := m.buildAuthMethods(host, hostVars, currentUser.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to build auth methods for host %s: %w", host, err)
	}

	// Configure host key checking based on config
	hostKeyCallback := m.buildHostKeyCallback(host)

	// Get connection timeout from config or use default
	timeout := 30 * time.Second

	config := &ssh.ClientConfig{
		User:            currentUser.Username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         timeout,
		ClientVersion:   "SSH-2.0-spage",
	}

	// Optionally check what authentication methods the server supports (skip when jump_host == "none")
	if m.cfg == nil || m.cfg.SSH.JumpHost != "none" {
		supportedMethods, err := m.getServerAuthMethods(host, currentUser.Username)
		if err != nil {
			common.LogWarn("Failed to detect server authentication methods", map[string]interface{}{
				"host":  host,
				"error": err.Error(),
			})
		} else {
			common.LogDebug("Server supports authentication methods", map[string]interface{}{
				"host":              host,
				"methods":           supportedMethods,
				"supports_password": contains(supportedMethods, "password"),
			})
		}
	} else {
		common.LogDebug("Skipping server auth method detection because jump host is explicitly 'none'", map[string]interface{}{"host": host})
	}

	return config, nil
}

// buildAuthMethods creates authentication methods based on configuration
func (m *Manager) buildAuthMethods(host string, hostVars map[string]interface{}, username string) ([]ssh.AuthMethod, error) {
	var authMethods []ssh.AuthMethod

	// Skip server method detection if jump_host is "none" to avoid interference
	var supportedMethods []string
	if m.cfg != nil && m.cfg.SSH.JumpHost == "none" {
		common.LogDebug("Jump host disabled with 'none', skipping server auth method detection to allow normal SSH flow", map[string]interface{}{
			"host": host,
		})
		supportedMethods = nil
	} else {
		// Check if we need server method detection
		// Only detect if we have methods that might not be supported (like password)
		configMethods := m.getConfiguredAuthMethods()
		needsDetection := m.needsServerMethodDetection(configMethods)

		if !needsDetection {
			common.LogDebug("Skipping server auth method detection - all configured methods are always available", map[string]interface{}{
				"host":    host,
				"methods": configMethods,
			})
			supportedMethods = nil
		} else {
			// Check what the server actually supports first
			var err error
			supportedMethods, err = m.getServerAuthMethods(host, username)
			if err != nil {
				common.LogWarn("Failed to detect server authentication methods, proceeding with configured methods", map[string]interface{}{
					"host":  host,
					"error": err.Error(),
				})
				supportedMethods = nil
			} else {
				common.LogDebug("Server supports authentication methods", map[string]interface{}{
					"host":              host,
					"methods":           supportedMethods,
					"supports_password": contains(supportedMethods, "password"),
				})
			}
		}
	}

	// Get authentication method order from config or use defaults
	var methods []string
	if m.cfg != nil && len(m.cfg.SSH.Auth.Methods) > 0 {
		methods = m.cfg.SSH.Auth.Methods
	} else {
		methods = []string{"publickey", "password"}
	}

	// Build auth methods in the configured order
	for _, method := range methods {
		// Skip methods not supported by server if we know what it supports
		if supportedMethods != nil && !contains(supportedMethods, method) && method != "publickey" {
			common.LogDebug("Skipping unsupported authentication method", map[string]interface{}{
				"host":      host,
				"method":    method,
				"supported": supportedMethods,
			})
			continue
		}

		switch method {
		case "publickey":
			if pubkeyMethods := m.buildPublicKeyAuth(host, hostVars); len(pubkeyMethods) > 0 {
				authMethods = append(authMethods, pubkeyMethods...)
			}
		case "password":
			if m.isPasswordAuthEnabled() && (supportedMethods == nil || contains(supportedMethods, "password") || contains(supportedMethods, "keyboard-interactive")) {
				passwordAuth := ssh.PasswordCallback(func() (string, error) {
					common.LogInfo("Password callback invoked - prompting user", map[string]interface{}{
						"host": host,
					})
					return promptForPassword(host)
				})
				authMethods = append(authMethods, passwordAuth)
				// Also add keyboard-interactive fallback for servers that expose only that method
				authMethods = append(authMethods, m.buildKeyboardInteractiveAuth(host))
			}
		case "keyboard-interactive":
			// Allow keyboard-interactive when enabled in config
			if m.cfg == nil || m.cfg.SSH.Auth.KeyboardAuth {
				authMethods = append(authMethods, m.buildKeyboardInteractiveAuth(host))
			}
		case "none":
			if m.cfg != nil && m.cfg.SSH.Auth.NoneAuth {
				authMethods = append(authMethods, ssh.Password("")) // "none" auth with empty password
			}
		}
	}

	// Fallback if no methods are configured
	if len(authMethods) == 0 {
		common.LogWarn("No authentication methods configured, using defaults", map[string]interface{}{"host": host})
		if pubkeyMethods := m.buildPublicKeyAuth(host, hostVars); len(pubkeyMethods) > 0 {
			authMethods = append(authMethods, pubkeyMethods...)
		}
		if m.isPasswordAuthEnabled() {
			passwordAuth := ssh.PasswordCallback(func() (string, error) {
				return promptForPassword(host)
			})
			authMethods = append(authMethods, passwordAuth)
		}
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no SSH authentication methods available for host %s", host)
	}

	return authMethods, nil
}

// buildPublicKeyAuth builds public key authentication methods
func (m *Manager) buildPublicKeyAuth(host string, hostVars map[string]interface{}) []ssh.AuthMethod {
	var authMethods []ssh.AuthMethod

	// Check for ansible_ssh_private_key_file in host variables
	if hostVars != nil {
		if privateKeyPath, exists := hostVars["ansible_ssh_private_key_file"]; exists {
			if keyPath, ok := privateKeyPath.(string); ok && keyPath != "" {
				if method := m.loadPrivateKeyFile(keyPath, host); method != nil {
					authMethods = append(authMethods, method)
				}
			}
		}
	}

	// Load specific public key files from config
	if m.cfg != nil && len(m.cfg.SSH.Auth.PublicKeys) > 0 {
		for _, keyPath := range m.cfg.SSH.Auth.PublicKeys {
			if method := m.loadPrivateKeyFile(keyPath, host); method != nil {
				authMethods = append(authMethods, method)
			}
		}
	}

	// Add SSH agent if available (unless identities_only is set)
	if m.cfg == nil || !m.cfg.SSH.Auth.IdentitiesOnly {
		if agentMethod := m.buildSSHAgentAuth(host); agentMethod != nil {
			authMethods = append(authMethods, agentMethod)
		}
	}

	return authMethods
}

// loadPrivateKeyFile loads a private key from file
func (m *Manager) loadPrivateKeyFile(keyPath, host string) ssh.AuthMethod {
	// Expand ~ to home directory if needed
	if strings.HasPrefix(keyPath, "~/") {
		if homeDir, err := os.UserHomeDir(); err == nil {
			keyPath = strings.Replace(keyPath, "~", homeDir, 1)
		}
	}

	// Load private key from file
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		common.LogWarn("Failed to read SSH private key file", map[string]interface{}{
			"host":     host,
			"key_path": keyPath,
			"error":    err.Error(),
		})
		return nil
	}

	// Try to parse the private key
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		common.LogWarn("Failed to parse SSH private key file", map[string]interface{}{
			"host":     host,
			"key_path": keyPath,
			"error":    err.Error(),
		})
		return nil
	}

	return ssh.PublicKeys(signer)
}

// buildSSHAgentAuth builds SSH agent authentication if available
func (m *Manager) buildSSHAgentAuth(host string) ssh.AuthMethod {
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		return nil
	}

	conn, err := net.Dial("unix", socket)
	if err != nil {
		common.LogWarn("Failed to connect to SSH agent", map[string]interface{}{
			"host":  host,
			"error": err.Error(),
		})
		return nil
	}

	agentClient := agent.NewClient(conn)
	common.LogDebug("SSH agent available, adding public key authentication", map[string]interface{}{
		"host": host,
	})

	return ssh.PublicKeysCallback(agentClient.Signers)
}

// buildKeyboardInteractiveAuth builds a keyboard-interactive auth method that prompts the user
func (m *Manager) buildKeyboardInteractiveAuth(host string) ssh.AuthMethod {
	challenge := func(user, instruction string, questions []string, echos []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i, q := range questions {
			prompt := strings.TrimSpace(q)
			if prompt == "" {
				prompt = "Password"
			}
			fullPrompt := fmt.Sprintf("%s for %s: ", prompt, host)
			if echos[i] {
				// Visible input
				fmt.Print(fullPrompt)
				var input string
				if _, err := fmt.Scanln(&input); err != nil {
					return nil, err
				}
				answers[i] = strings.TrimSpace(input)
			} else {
				// Secret (no-echo) input
				fmt.Print(fullPrompt)
				pwBytes, err := term.ReadPassword(int(syscall.Stdin))
				fmt.Println()
				if err != nil {
					return nil, err
				}
				answers[i] = string(pwBytes)
			}
		}
		return answers, nil
	}
	return ssh.KeyboardInteractive(challenge)
}

// isPasswordAuthEnabled checks if password authentication is enabled
func (m *Manager) isPasswordAuthEnabled() bool {
	if m.cfg == nil {
		return true // Default behavior for backwards compatibility
	}
	return m.cfg.SSH.Auth.PasswordAuth
}

// buildHostKeyCallback creates the appropriate host key callback
func (m *Manager) buildHostKeyCallback(host string) ssh.HostKeyCallback {
	if m.cfg == nil {
		return ssh.InsecureIgnoreHostKey()
	}

	// Check security configuration first, then fallback to legacy
	hostKeyChecking := "yes"
	if m.cfg != nil && !m.cfg.HostKeyChecking {
		hostKeyChecking = "no"
	}

	switch hostKeyChecking {
	case "no":
		return ssh.InsecureIgnoreHostKey()
	case "ask":
		// TODO: Implement interactive host key verification
		common.LogWarn("Host key checking 'ask' mode not implemented, using insecure", map[string]interface{}{"host": host})
		return ssh.InsecureIgnoreHostKey()
	default: // "yes"
		// TODO: Implement proper host key checking with known_hosts file
		common.LogWarn("Host key checking enabled but not fully implemented, using insecure host key verification", map[string]interface{}{"host": host})
		return ssh.InsecureIgnoreHostKey()
	}
}

// promptForPassword prompts the user for a password for the given host
func promptForPassword(host string) (string, error) {
	// Check if we're running in a non-interactive environment
	if !term.IsTerminal(int(syscall.Stdin)) {
		return "", fmt.Errorf("password required but running in non-interactive mode for host %s", host)
	}

	fmt.Printf("SSH key authentication failed. Enter password for %s: ", host)

	// Read password without echoing to terminal
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", fmt.Errorf("failed to read password for host %s: %w", host, err)
	}

	// Print newline after password input
	fmt.Println()

	password := string(passwordBytes)
	if password == "" {
		return "", fmt.Errorf("empty password provided for host %s", host)
	}

	return password, nil
}

// getServerAuthMethods checks what authentication methods the SSH server supports
func (m *Manager) getServerAuthMethods(host, username string) ([]string, error) {
	// Check cache first
	if methods, found := m.getCachedAuthMethods(host); found {
		return methods, nil
	}

	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			// Use a dummy auth method that will always fail
			ssh.Password("dummy"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	_, err := ssh.Dial("tcp", net.JoinHostPort(host, "22"), config)
	if err != nil {
		// Parse the error message to extract supported methods
		errorStr := err.Error()
		if strings.Contains(errorStr, "no supported methods remain") {
			// Extract methods from error message like "attempted methods [none publickey]"
			if idx := strings.Index(errorStr, "attempted methods ["); idx != -1 {
				start := idx + len("attempted methods [")
				if end := strings.Index(errorStr[start:], "]"); end != -1 {
					methodsStr := errorStr[start : start+end]
					methods := strings.Fields(methodsStr)

					// Cache the result
					m.authCacheMu.Lock()
					m.authMethodCache[host] = &authMethodCache{
						methods: methods,
						expires: time.Now().Add(10 * time.Minute), // Cache for 10 minutes
					}
					m.authCacheMu.Unlock()

					return methods, nil
				}
			}
		}
		return nil, fmt.Errorf("failed to detect authentication methods: %w", err)
	}

	// If connection succeeded, password is supported
	methods := []string{"password"}

	// Cache the result
	m.authCacheMu.Lock()
	m.authMethodCache[host] = &authMethodCache{
		methods: methods,
		expires: time.Now().Add(10 * time.Minute), // Cache for 10 minutes
	}
	m.authCacheMu.Unlock()

	return methods, nil
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Close closes all SSH pools
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for host, pool := range m.pools {
		pool.Close()
		delete(m.pools, host)
	}

	// Clear authentication method cache when closing
	m.clearAuthMethodCache("")

	if len(errs) > 0 {
		return fmt.Errorf("SSH pool manager close errors: %v", errs)
	}
	return nil
}

// CloseHost closes the SSH pool for a specific host
func (m *Manager) CloseHost(host string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if pool, exists := m.pools[host]; exists {
		pool.Close()
		delete(m.pools, host)
	}

	// Clear authentication method cache for this host
	m.clearAuthMethodCache(host)
}

// getConfiguredAuthMethods returns the list of configured authentication methods
func (m *Manager) getConfiguredAuthMethods() []string {
	if m.cfg != nil && len(m.cfg.SSH.Auth.Methods) > 0 {
		return m.cfg.SSH.Auth.Methods
	}
	return []string{"publickey", "password"}
}

// needsServerMethodDetection checks if server method detection is needed
// Returns false if all configured methods are always available (like publickey)
func (m *Manager) needsServerMethodDetection(methods []string) bool {
	for _, method := range methods {
		// Methods that might not be supported by all servers
		if method == "password" || method == "keyboard-interactive" || method == "gssapi-with-mic" || method == "none" {
			return true
		}
	}
	return false
}

// clearAuthMethodCache clears the authentication method cache for a specific host or all hosts
func (m *Manager) clearAuthMethodCache(host string) {
	m.authCacheMu.Lock()
	defer m.authCacheMu.Unlock()

	if host == "" {
		// Clear all cache entries
		m.authMethodCache = make(map[string]*authMethodCache)
	} else {
		// Clear cache for specific host
		delete(m.authMethodCache, host)
	}
}

// ClearAuthMethodCache clears the authentication method cache for a specific host or all hosts
// This is useful for testing or when you want to force re-detection of authentication methods
func (m *Manager) ClearAuthMethodCache(host string) {
	m.clearAuthMethodCache(host)
	if host == "" {
		common.LogDebug("Cleared all SSH authentication method caches", map[string]interface{}{})
	} else {
		common.LogDebug("Cleared SSH authentication method cache for host", map[string]interface{}{
			"host": host,
		})
	}
}

// getCachedAuthMethods returns cached authentication methods for a host if available
func (m *Manager) getCachedAuthMethods(host string) ([]string, bool) {
	m.authCacheMu.RLock()
	defer m.authCacheMu.RUnlock()

	if cache, exists := m.authMethodCache[host]; exists && cache.expires.After(time.Now()) {
		common.LogDebug("Using cached authentication methods for host", map[string]interface{}{
			"host":    host,
			"methods": cache.methods,
			"expires": cache.expires,
		})
		return cache.methods, true
	}
	return nil, false
}
