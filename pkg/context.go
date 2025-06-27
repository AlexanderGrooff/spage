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
	"github.com/AlexanderGrooff/spage/pkg/runtime"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type HostContext struct {
	Host      *Host
	Facts     *sync.Map
	History   *sync.Map
	sshClient *ssh.Client
}

func InitializeHostContext(host *Host) (*HostContext, error) {
	hc := &HostContext{
		Host:    host,
		Facts:   new(sync.Map),
		History: new(sync.Map),
	}

	if !host.IsLocal {
		socket := os.Getenv("SSH_AUTH_SOCK")
		if socket == "" {
			return nil, fmt.Errorf("SSH_AUTH_SOCK environment variable not set, cannot connect to remote host %s", host.Host)
		}
		conn, err := net.Dial("unix", socket)
		if err != nil {
			return nil, fmt.Errorf("failed to open SSH_AUTH_SOCK for host %s: %v", host.Host, err)
		}
		// Note: We're not closing the 'conn' here because the agentClient needs it.
		// The agentClient itself doesn't have a Close method. The underlying connection
		// might be closed when the ssh.Client is closed, or it might persist.
		// This is typical usage for ssh agent.

		agentClient := agent.NewClient(conn)
		currentUser, err := user.Current() // Consider making username configurable
		if err != nil {
			// We might not want to fail initialization just because we can't get the current user
			// Log a warning and proceed? Or require explicit user configuration?
			// For now, return error.
			return nil, fmt.Errorf("failed to get current user for SSH connection to %s: %v", host.Host, err)
		}

		config := &ssh.ClientConfig{
			User: currentUser.Username, // Use current user's username
			Auth: []ssh.AuthMethod{
				ssh.PublicKeysCallback(agentClient.Signers), // Use keys from ssh-agent
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: Make host key checking configurable
		}

		// TODO: Make SSH port configurable
		addr := net.JoinHostPort(host.Host, "22")
		client, err := ssh.Dial("tcp", addr, config)
		if err != nil {
			// Ensure conn is closed if Dial fails? The Dial usually handles underlying connection closure on error.
			return nil, fmt.Errorf("failed to dial SSH host %s: %w", host.Host, err)
		}
		hc.sshClient = client
		common.DebugOutput("Established SSH connection to %s", host.Host)
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
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
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
	res, err := jinja.EvaluateExpression(s, closure.GetFacts())
	common.DebugOutput("Evaluated expression %q -> %v with facts: %v. Error: %v", s, res, closure.GetFacts(), err)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate expression: %v", err)
	}
	return res, nil
}

// TemplateString processes a Jinja2 template string with provided variables.
func TemplateString(s string, closure *Closure) (string, error) {
	if s == "" {
		return "", nil
	}
	facts := closure.GetFacts()
	res, err := jinja.TemplateString(s, facts)
	if err != nil {
		return "", fmt.Errorf("failed to template string: %v", err)
	}
	if s != res {
		common.DebugOutput("Templated %q into %q with facts: %v", s, res, closure.GetFacts())
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

func (c *HostContext) Close() error {
	if c.sshClient != nil {
		err := c.sshClient.Close()
		c.sshClient = nil // Ensure it's marked as closed
		return err
	}
	return nil
}
