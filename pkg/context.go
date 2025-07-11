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

	"github.com/AlexanderGrooff/jinja-go"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
	"github.com/AlexanderGrooff/spage/pkg/runtime"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type HostContext struct {
	Host           *Host
	Facts          *sync.Map
	History        *sync.Map
	HandlerTracker *HandlerTracker
	sshClient      *ssh.Client
}

func InitializeHostContext(host *Host, cfg *config.Config) (*HostContext, error) {
	hc := &HostContext{
		Host:           host,
		Facts:          new(sync.Map),
		History:        new(sync.Map),
		HandlerTracker: nil, // Will be initialized later with handlers
	}

	if !host.IsLocal {
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
		if cfg != nil && cfg.HostKeyChecking {
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
		}

		// TODO: Make SSH port configurable
		addr := net.JoinHostPort(host.Host, "22")
		client, err := ssh.Dial("tcp", addr, config)
		if err != nil {
			// Ensure conn is closed if Dial fails? The Dial usually handles underlying connection closure on error.
			return nil, fmt.Errorf("failed to dial SSH host %s: %w", host.Host, err)
		}
		hc.sshClient = client
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
	if c.sshClient == nil {
		return nil, fmt.Errorf("ssh client not initialized for remote host %s", c.Host.Host)
	}
	return runtime.ReadRemoteFileBytes(c.sshClient, filename)
}

func (c *HostContext) WriteFile(filename, contents, username string) error {
	// Note: username is currently ignored for SFTP operations
	if c.Host.IsLocal {
		return runtime.WriteLocalFile(filename, contents)
	}
	if c.sshClient == nil {
		return fmt.Errorf("ssh client not initialized for remote host %s", c.Host.Host)
	}
	return runtime.WriteRemoteFile(c.sshClient, filename, contents)
}

func (c *HostContext) Copy(src, dst string) error {
	if c.Host.IsLocal {
		return runtime.CopyLocal(src, dst)
	}
	if c.sshClient == nil {
		return fmt.Errorf("ssh client not initialized for remote host %s", c.Host.Host)
	}
	return runtime.CopyRemote(c.sshClient, src, dst)
}

func (c *HostContext) SetFileMode(path, mode, username string) error {
	// Note: username is currently ignored for SFTP operations
	if c.Host.IsLocal {
		return runtime.SetLocalFileMode(path, mode)
	}
	if c.sshClient == nil {
		return fmt.Errorf("ssh client not initialized for remote host %s", c.Host.Host)
	}
	return runtime.SetRemoteFileMode(c.sshClient, path, mode)
}

// Stat retrieves file info. For remote hosts, it uses SFTP Lstat (no follow).
// For local hosts, it uses os.Stat (follow=true) or os.Lstat (follow=false).
func (c *HostContext) Stat(path string, follow bool) (os.FileInfo, error) {
	// Note: runAs/username associated with the HostContext is currently ignored for SFTP/local stat operations.
	if c.Host.IsLocal {
		return runtime.StatLocal(path, follow)
	}

	// Remote host logic
	if c.sshClient == nil {
		return nil, fmt.Errorf("ssh client not initialized for remote host %s", c.Host.Host)
	}
	if follow {
		// TODO: Implement following links for remote SFTP stat if needed.
		// This could involve sftp.ReadLink + sftp.Stat or just sftp.Stat
		common.LogWarn("Following links is not currently implemented for remote stat, using Lstat instead.", map[string]interface{}{"path": path, "host": c.Host.Host})
		// Fallthrough to Lstat for now
	}
	// SFTP StatRemote currently always uses Lstat (no follow)
	return runtime.StatRemote(c.sshClient, path)
}

func (c *HostContext) RunCommand(command, username string) (int, string, string, error) {
	// TODO: why was this necessary?
	//if username == "" {
	//	user, err := user.Current()
	//	if err != nil {
	//		return -1, "", "", fmt.Errorf("failed to get current user: %v", err)
	//	}
	//	username = user.Username
	//}
	if c.Host.IsLocal {
		return runtime.RunLocalCommand(command, username)
	}
	// Ensure SSH client exists for remote commands
	if c.sshClient == nil {
		return -1, "", "", fmt.Errorf("ssh client not initialized for remote host %s", c.Host.Host)
	}
	return runtime.RunRemoteCommand(c.sshClient, command, username)
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
	if c.sshClient != nil {
		err := c.sshClient.Close()
		c.sshClient = nil // Ensure it's marked as closed
		return err
	}
	return nil
}
