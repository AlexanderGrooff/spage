package pkg

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/runtime"

	"github.com/flosch/pongo2"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// No longer using type aliases for History and Facts

// MergeFacts adds all key-value pairs from 'other' into 'target'.
// It assumes both are *sync.Map.
func MergeFacts(target, other *sync.Map) {
	if target == nil || other == nil {
		// Handle nil maps appropriately, perhaps return an error or initialize
		return
	}
	other.Range(func(key, value interface{}) bool {
		target.Store(key, value)
		return true
	})
}

// AddFact stores a single key-value pair into the target *sync.Map.
func AddFact(target *sync.Map, k string, v interface{}) {
	if target == nil {
		// Handle nil map
		return
	}
	target.Store(k, v)
}

// FactsToJinja2 converts a *sync.Map (representing Facts) into a pongo2.Context.
func FactsToJinja2(facts *sync.Map) pongo2.Context {
	ctx := pongo2.Context{}
	if facts == nil {
		return ctx // Return empty context if map is nil
	}
	facts.Range(func(key, value interface{}) bool {
		// Ensure key is a string for pongo2.Context
		if k, ok := key.(string); ok {
			ctx[k] = value
		} else {
			// Optionally log or handle non-string keys
			common.LogWarn("Non-string key found in Facts map during Jinja2 conversion", map[string]interface{}{"key": key})
		}
		return true
	})
	return ctx
}

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

func (c *HostContext) RunCommand(command, username string) (string, string, error) {
	// TODO: why was this necessary?
	//if username == "" {
	//	user, err := user.Current()
	//	if err != nil {
	//		return "", "", fmt.Errorf("failed to get current user: %v", err)
	//	}
	//	username = user.Username
	//}
	if c.Host.IsLocal {
		return runtime.RunLocalCommand(command, username)
	}
	// Ensure SSH client exists for remote commands
	if c.sshClient == nil {
		return "", "", fmt.Errorf("ssh client not initialized for remote host %s", c.Host.Host)
	}
	return runtime.RunRemoteCommand(c.sshClient, command, username)
}

func EvaluateExpression(s string, additionalVars ...*sync.Map) (string, error) {
	// TODO: this is a hack to evaluate a string as a Jinja2 template
	return TemplateString(fmt.Sprintf("{{ %s }}", s), additionalVars...)
}

// TemplateString processes a Jinja2 template string with provided variables.
// additionalVars should be a slice of *sync.Map.
func TemplateString(s string, additionalVars ...*sync.Map) (string, error) {
	if s == "" {
		return "", nil
	}
	tmpl, err := pongo2.FromString(s)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %v", err)
	}

	// Merge all provided variable maps into one *sync.Map
	allVars := new(sync.Map)
	for _, v := range additionalVars {
		MergeFacts(allVars, v) // Use the standalone MergeFacts function
	}
	var buf bytes.Buffer
	// Convert the final *sync.Map to pongo2.Context before executing
	facts := FactsToJinja2(allVars)
	if err := tmpl.ExecuteWriter(facts, &buf); err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}
	res := buf.String()
	common.DebugOutput("Templated %q into %q with facts: %v", s, res, facts)
	return res, nil
}

func getVariablesFromJinjaString(jinjaString string) []string {
	// TODO: this parses the string within jinja brackets. Add the rest of the Jinja grammar
	filterApplication := regexp.MustCompile(`(.+) | (.+)`)
	attributeVariable := regexp.MustCompile(`(.+)\.(.+)`)

	var vars []string
	if filterApplication.MatchString(jinjaString) {
		common.DebugOutput("Found Jinja filter %v", jinjaString)
		for _, match := range filterApplication.FindAllStringSubmatch(jinjaString, -1) {
			vars = append(vars, getVariablesFromJinjaString(match[1])...)
		}
		return vars
	}
	if attributeVariable.MatchString(jinjaString) {
		common.DebugOutput("Found Jinja subattribute %v", jinjaString)
		for _, match := range attributeVariable.FindAllStringSubmatch(jinjaString, -1) {
			vars = append(vars, getVariablesFromJinjaString(match[1])...)
		}
		return vars
	}
	return append(vars, jinjaString)
}

func GetVariableUsageFromTemplate(s string) []string {
	everythingBetweenBrackets := regexp.MustCompile(`{{\s*([^{}\s]+)\s*}}`)
	matches := everythingBetweenBrackets.FindAllStringSubmatch(s, -1)

	var vars []string
	for _, match := range matches {
		jinjaVariables := getVariablesFromJinjaString(match[1])
		vars = append(vars, jinjaVariables...)
	}
	if len(vars) > 0 {
		common.DebugOutput("Found variables %v in %v", vars, s)
	}
	return vars
}

// Helper function (might need adjustment based on actual ModuleOutput type)
// This is a placeholder, assuming ModuleOutput can be stored directly.
// If ModuleOutput needs specific conversion before storing in sync.Map, adjust this.
func OutputToFacts(output ModuleOutput) interface{} {
	// Placeholder: Directly return the output.
	// If ModuleOutput is complex, you might need to extract specific fields.
	return output
}

// IsExpressionTruthy evaluates a rendered expression string according to Jinja2/Ansible truthiness rules.
// Explicit "true"/"false" (case-insensitive) are respected.
// Empty strings are false.
// All other non-empty strings are true.
func IsExpressionTruthy(renderedExpr string) bool {
	trimmedResult := strings.TrimSpace(renderedExpr)
	lowerTrimmedResult := strings.ToLower(trimmedResult)

	parsedBool, err := strconv.ParseBool(lowerTrimmedResult)
	if err == nil {
		// Explicit true/false
		return parsedBool
	}

	// Not "true" or "false", evaluate truthiness:
	// Empty string is false, everything else is true.
	return trimmedResult != ""
}

func (c *HostContext) Close() error {
	if c.sshClient != nil {
		common.DebugOutput("Closing SSH connection to %s", c.Host.Host)
		err := c.sshClient.Close()
		c.sshClient = nil // Ensure it's marked as closed
		return err
	}
	return nil
}
