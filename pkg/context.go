package pkg

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/AlexanderGrooff/jinja-go"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
	"github.com/AlexanderGrooff/spage/pkg/runtime"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// ConnectionPool manages SSH connections and SFTP clients for better performance
type ConnectionPool struct {
	sshClient  *ssh.Client
	sftpClient *runtime.SftpClient
	mu         sync.RWMutex
	lastUsed   time.Time
	timeout    time.Duration
}

// NewConnectionPool creates a new connection pool with timeout
func NewConnectionPool(timeout time.Duration) *ConnectionPool {
	return &ConnectionPool{
		timeout: timeout,
	}
}

// IsExpired checks if the connection pool has expired
func (cp *ConnectionPool) IsExpired() bool {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return time.Since(cp.lastUsed) > cp.timeout
}

// Close closes all connections in the pool
func (cp *ConnectionPool) Close() error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	var errs []error

	if cp.sftpClient != nil {
		if err := cp.sftpClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close SFTP client: %w", err))
		}
		cp.sftpClient = nil
	}

	if cp.sshClient != nil {
		if err := cp.sshClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close SSH client: %w", err))
		}
		cp.sshClient = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("connection pool close errors: %v", errs)
	}
	return nil
}

// HostContext represents the context for a specific host during playbook execution.
// It contains host-specific data like facts, variables, and SSH connections.
type HostContext struct {
	Host           *Host
	Facts          *sync.Map
	History        *sync.Map
	HandlerTracker *HandlerTracker
	connectionPool *ConnectionPool
	mu             sync.RWMutex
}

// GetOrCreateSSHClient returns an existing SSH client or creates a new one
func (c *HostContext) GetOrCreateSSHClient() (*ssh.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we have a valid connection pool
	if c.connectionPool != nil && c.connectionPool.sshClient != nil && !c.connectionPool.IsExpired() {
		c.connectionPool.lastUsed = time.Now()
		return c.connectionPool.sshClient, nil
	}

	// Close existing connections if they exist but are expired
	if c.connectionPool != nil {
		err := c.connectionPool.Close()
		if err != nil {
			common.LogWarn("Failed to close connection pool", map[string]interface{}{
				"host":  c.Host.Host,
				"error": err.Error(),
			})
		}
	}

	// Create new SSH client
	client, err := c.createSSHClient()
	if err != nil {
		return nil, err
	}

	if c.connectionPool == nil {
		c.connectionPool = NewConnectionPool(5 * time.Minute) // 5 minute timeout
	}
	c.connectionPool.sshClient = client
	c.connectionPool.lastUsed = time.Now()

	return client, nil
}

// GetOrCreateSftpClient returns an existing SFTP client or creates a new one
func (c *HostContext) GetOrCreateSftpClient() (*runtime.SftpClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we have a valid connection pool
	if c.connectionPool != nil && c.connectionPool.sftpClient != nil && !c.connectionPool.IsExpired() {
		c.connectionPool.lastUsed = time.Now()
		return c.connectionPool.sftpClient, nil
	}

	// Close existing connections if they exist but are expired
	if c.connectionPool != nil {
		err := c.connectionPool.Close()
		if err != nil {
			common.LogWarn("Failed to close connection pool", map[string]interface{}{
				"host":  c.Host.Host,
				"error": err.Error(),
			})
		}
	}

	// Create new SSH client first (without holding the lock)
	sshClient, err := c.createSSHClient()
	if err != nil {
		return nil, err
	}

	// Create new SFTP client
	sftpClient, err := runtime.NewSftpClient(sshClient)
	if err != nil {
		return nil, err
	}

	if c.connectionPool == nil {
		c.connectionPool = NewConnectionPool(5 * time.Minute) // 5 minute timeout
	}
	c.connectionPool.sshClient = sshClient
	c.connectionPool.sftpClient = sftpClient
	c.connectionPool.lastUsed = time.Now()

	return sftpClient, nil
}

// createSSHClient creates a new SSH client connection
func (c *HostContext) createSSHClient() (*ssh.Client, error) {
	host := c.Host

	// Check if ansible_ssh_private_key_file is provided in host variables
	var hasPrivateKeyFile bool
	var privateKeyAuthMethod ssh.AuthMethod
	if host.Vars != nil {
		if privateKeyPath, exists := host.Vars["ansible_ssh_private_key_file"]; exists {
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
						"host":     host.Host,
						"key_path": keyPath,
						"error":    err.Error(),
					})
				} else {
					// Try to parse the private key
					signer, err := ssh.ParsePrivateKey(keyBytes)
					if err != nil {
						common.LogWarn("Failed to parse SSH private key file", map[string]interface{}{
							"host":     host.Host,
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
					"host":  host.Host,
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
				"host":  host.Host,
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
			return nil, fmt.Errorf("SSH private key file specified but failed to load, and SSH_AUTH_SOCK not available for host %s", host.Host)
		} else {
			return nil, fmt.Errorf("no SSH authentication methods available for host %s: SSH_AUTH_SOCK environment variable not set and no ansible_ssh_private_key_file specified", host.Host)
		}
	}

	currentUser, err := user.Current() // Consider making username configurable
	if err != nil {
		// We might not want to fail initialization just because we can't get the current user
		// Log a warning and proceed? Or require explicit user configuration?
		// For now, return error.
		return nil, fmt.Errorf("failed to get current user for SSH connection to %s: %v", host.Host, err)
	}

	// Configure host key checking based on config
	var hostKeyCallback ssh.HostKeyCallback
	if c.Host.Config != nil && c.Host.Config.HostKeyChecking {
		// TODO: Implement proper host key checking with known_hosts file
		// For now, this will accept any host key but warn about it
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
		common.LogWarn("Host key checking is enabled but not fully implemented, using insecure host key verification", map[string]interface{}{"host": host.Host})
	} else {
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	config := &ssh.ClientConfig{
		User:            currentUser.Username, // Use current user's username
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		// Add connection timeout
		Timeout: 30 * time.Second,
	}

	// TODO: Make SSH port configurable
	addr := net.JoinHostPort(host.Host, "22")
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		// Ensure conn is closed if Dial fails? The Dial usually handles underlying connection closure on error.
		return nil, fmt.Errorf("failed to dial SSH host %s: %w", host.Host, err)
	}
	return client, nil
}

func InitializeHostContext(host *Host, cfg *config.Config) (*HostContext, error) {
	hc := &HostContext{
		Host:           host,
		Facts:          new(sync.Map),
		History:        new(sync.Map),
		HandlerTracker: nil, // Will be initialized later with handlers
	}

	// Set config on host for later use
	host.Config = cfg

	// For local hosts, we don't need SSH connection
	if !host.IsLocal {
		// Test SSH connection during initialization
		_, err := hc.GetOrCreateSSHClient()
		if err != nil {
			return nil, err
		}
	}

	return hc, nil
}

func ReadTemplateFile(filename string) (string, error) {
	if filename[0] != '/' {
		return ReadLocalFile("templates/" + filename)
	}
	return ReadLocalFile(filename)
}

func ReadLocalFile(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filename, err)
	}
	return string(data), nil
}

func (c *HostContext) ReadFile(filename string, username string) (string, error) {
	bytes, err := c.ReadFileBytes(filename, username)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func (c *HostContext) ReadFileBytes(filename string, username string) ([]byte, error) {
	// Note: username is currently ignored for SFTP operations, as the connection
	// uses the user established during InitializeHostContext.
	if c.Host.IsLocal {
		return runtime.ReadLocalFileBytes(filename)
	}

	sftpClient, err := c.GetOrCreateSftpClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get SFTP client for remote host %s: %w", c.Host.Host, err)
	}
	return runtime.ReadRemoteFileBytesWithPooledClient(sftpClient, filename)
}

func (c *HostContext) WriteFile(filename, contents, username string) error {
	// Note: username is currently ignored for SFTP operations, as the connection
	// uses the user established during InitializeHostContext.
	if c.Host.IsLocal {
		return runtime.WriteLocalFile(filename, contents)
	}

	sftpClient, err := c.GetOrCreateSftpClient()
	if err != nil {
		return fmt.Errorf("failed to get SFTP client for remote host %s: %w", c.Host.Host, err)
	}
	return runtime.WriteRemoteFileWithPooledClient(sftpClient, filename, contents)
}

func (c *HostContext) Copy(src, dst string) error {
	if c.Host.IsLocal {
		return runtime.CopyLocal(src, dst)
	}

	sftpClient, err := c.GetOrCreateSftpClient()
	if err != nil {
		return fmt.Errorf("failed to get SFTP client for remote host %s: %w", c.Host.Host, err)
	}
	return runtime.CopyRemoteWithPooledClient(sftpClient, src, dst)
}

func (c *HostContext) SetFileMode(path, mode, username string) error {
	if c.Host.IsLocal {
		return runtime.SetLocalFileMode(path, mode)
	}

	sftpClient, err := c.GetOrCreateSftpClient()
	if err != nil {
		return fmt.Errorf("failed to get SFTP client for remote host %s: %w", c.Host.Host, err)
	}
	return runtime.SetRemoteFileModeWithPooledClient(sftpClient, path, mode)
}

func (c *HostContext) Stat(path string, follow bool) (os.FileInfo, error) {
	if c.Host.IsLocal {
		if follow {
			return os.Stat(path)
		}
		return os.Lstat(path)
	}

	sftpClient, err := c.GetOrCreateSftpClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get SFTP client for remote host %s: %w", c.Host.Host, err)
	}
	return runtime.StatRemoteWithPooledClient(sftpClient, path)
}

func (c *HostContext) RunCommand(command, username string) (int, string, string, error) {
	if c.Host.IsLocal {
		return runtime.RunLocalCommand(command, username)
	}

	sshClient, err := c.GetOrCreateSSHClient()
	if err != nil {
		return -1, "", "", fmt.Errorf("ssh client not initialized for remote host %s: %w", c.Host.Host, err)
	}
	return runtime.RunRemoteCommand(sshClient, command, username)
}

func EvaluateExpression(s string, closure *Closure) (interface{}, error) {
	context := closure.GetFacts()
	res, err := jinja.EvaluateExpression(s, context)
	common.DebugOutput("Evaluated expression %q -> %v with facts: %v. Error: %v", s, res, context, err)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate expression: %w", err)
	}
	return res, nil
}

// TemplateString processes a Jinja2 template string with provided variables.
func TemplateString(s string, closure *Closure) (string, error) {
	if s == "" {
		return "", nil
	}
	context := closure.GetFacts()
	res, err := jinja.TemplateString(s, context)
	if err != nil {
		return "", fmt.Errorf("failed to template string: %w", err)
	}
	if s != res {
		common.DebugOutput("Templated %q into %q with facts: %v", s, res, context)
	}
	return res, nil
}

var jinjaKeywords = []string{"if", "for", "while", "with", "else", "elif", "endfor", "endwhile", "endwith", "endif", "not", "in", "is"}

func GetVariablesFromExpression(jinjaString string) []string {
	// TODO: this parses the string within jinja brackets. Add the rest of the Jinja grammar
	// This really is not sufficient, but it's a start. It doesn't cover things like:
	// - "string" in var
	// - 'string' in var
	filterApplication := regexp.MustCompile(`([\w_]+) \| (.+)`)
	attributeVariable := regexp.MustCompile(`([\w_]+)\.(.+)`)

	var vars []string
	if filterApplication.MatchString(jinjaString) {
		for _, match := range filterApplication.FindAllStringSubmatch(jinjaString, -1) {
			jinjaVar := strings.TrimSpace(match[1])
			if jinjaVar != "" && !slices.Contains(jinjaKeywords, jinjaVar) {
				vars = append(vars, GetVariablesFromExpression(jinjaVar)...)
			}
		}
		return vars
	}
	if attributeVariable.MatchString(jinjaString) {
		for _, match := range attributeVariable.FindAllStringSubmatch(jinjaString, -1) {
			jinjaVar := strings.TrimSpace(match[1])
			if jinjaVar != "" && !slices.Contains(jinjaKeywords, jinjaVar) {
				vars = append(vars, GetVariablesFromExpression(jinjaVar)...)
			}
		}
		return vars
	}
	return append(vars, jinjaString)
}

func GetVariableUsageFromTemplate(s string) []string {
	vars, err := jinja.ParseVariables(s)
	if err != nil {
		return nil
	}
	return vars
}

// InitializeHandlerTracker initializes the HandlerTracker with the provided handlers
func (c *HostContext) InitializeHandlerTracker(handlers []Task) {
	if c.HandlerTracker == nil {
		c.HandlerTracker = NewHandlerTracker(c.Host.Name, handlers)
	}
}

func (c *HostContext) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connectionPool != nil {
		return c.connectionPool.Close()
	}
	return nil
}
