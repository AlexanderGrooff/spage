package sshpool

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"strings"
	"sync"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
	desopssshpool "github.com/desops/sshpool"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Manager manages SSH connection pools for multiple hosts
type Manager struct {
	pools map[string]*desopssshpool.Pool
	mu    sync.RWMutex
	cfg   *config.Config
}

// NewManager creates a new SSH pool manager
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		pools: make(map[string]*desopssshpool.Pool),
		cfg:   cfg,
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
	// Create SSH client config
	sshConfig, err := m.createSSHConfig(host, hostVars)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH config for host %s: %w", host, err)
	}

	// Create pool configuration with reasonable defaults
	poolConfig := &desopssshpool.PoolConfig{
		Debug:             false, // TODO: Add debug field to config if needed
		MaxSessions:       10,    // Default SSH server limit
		MaxConnections:    5,     // Conservative default
		SessionCloseDelay: 20 * time.Millisecond,
	}

	// Create the pool
	pool := desopssshpool.New(sshConfig, poolConfig)
	return pool, nil
}

// createSSHConfig creates SSH client configuration for a host
func (m *Manager) createSSHConfig(host string, hostVars map[string]interface{}) (*ssh.ClientConfig, error) {
	// Check if ansible_ssh_private_key_file is provided in host variables
	var hasPrivateKeyFile bool
	var privateKeyAuthMethod ssh.AuthMethod
	if hostVars != nil {
		if privateKeyPath, exists := hostVars["ansible_ssh_private_key_file"]; exists {
			if keyPath, ok := privateKeyPath.(string); ok && keyPath != "" {
				// Expand ~ to home directory if needed
				if strings.HasPrefix(keyPath, "~/") {
					if homeDir, err := os.UserHomeDir(); err == nil {
						keyPath = strings.Replace(keyPath, "~", homeDir, 1)
					}
				}

				// Load private key from file
				keyBytes, err := os.ReadFile(keyPath)
				if err != nil {
					common.LogWarn("Failed to read SSH private key file specified in ansible_ssh_private_key_file", map[string]interface{}{
						"host":     host,
						"key_path": keyPath,
						"error":    err.Error(),
					})
				} else {
					// Try to parse the private key
					signer, err := ssh.ParsePrivateKey(keyBytes)
					if err != nil {
						common.LogWarn("Failed to parse SSH private key file", map[string]interface{}{
							"host":     host,
							"key_path": keyPath,
							"error":    err.Error(),
						})
					} else {
						privateKeyAuthMethod = ssh.PublicKeys(signer)
						hasPrivateKeyFile = true
					}
				}
			} else {
				common.LogWarn("ansible_ssh_private_key_file is not a string or is empty", map[string]interface{}{
					"host":  host,
					"value": privateKeyPath,
				})
			}
		}
	}

	// Prepare SSH auth methods
	var authMethods []ssh.AuthMethod

	// Add private key file auth method first if available
	if hasPrivateKeyFile {
		authMethods = append(authMethods, privateKeyAuthMethod)
	}

	// Try to add SSH agent auth method if available
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket != "" {
		conn, err := net.Dial("unix", socket)
		if err != nil {
			common.LogWarn("Failed to connect to SSH agent, continuing with other auth methods", map[string]interface{}{
				"host":  host,
				"error": err.Error(),
			})
		} else {
			agentClient := agent.NewClient(conn)
			authMethods = append(authMethods, ssh.PublicKeysCallback(agentClient.Signers))
		}
	}

	// Check if we have any auth methods
	if len(authMethods) == 0 {
		if hasPrivateKeyFile {
			return nil, fmt.Errorf("SSH private key file specified but failed to load, and SSH_AUTH_SOCK not available for host %s", host)
		} else {
			return nil, fmt.Errorf("no SSH authentication methods available for host %s: SSH_AUTH_SOCK environment variable not set and no ansible_ssh_private_key_file specified", host)
		}
	}

	currentUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user for SSH connection to %s: %v", host, err)
	}

	// Configure host key checking based on config
	var hostKeyCallback ssh.HostKeyCallback
	if m.cfg != nil && m.cfg.HostKeyChecking {
		// TODO: Implement proper host key checking with known_hosts file
		// For now, this will accept any host key but warn about it
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
		common.LogWarn("Host key checking is enabled but not fully implemented, using insecure host key verification", map[string]interface{}{"host": host})
	} else {
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	config := &ssh.ClientConfig{
		User:            currentUser.Username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         30 * time.Second,
	}

	return config, nil
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
}
