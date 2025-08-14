package pkg

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/AlexanderGrooff/jinja-go"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
	"github.com/AlexanderGrooff/spage/pkg/runtime"
	"github.com/AlexanderGrooff/spage/pkg/sshpool"
	desopssshpool "github.com/desops/sshpool"
)

// HostContext represents the context for a specific host during playbook execution.
// It contains host-specific data like facts, variables, and SSH connections.
type HostContext struct {
	Host           *Host
	Facts          *sync.Map
	History        *sync.Map
	HandlerTracker *HandlerTracker
	sshPoolManager *sshpool.Manager
	mu             sync.RWMutex
}

// Global source FS for reading playbook-relative assets (templates, copy src) when running from a bundle.
var sourceFSHolder struct {
	fs fs.FS
}

// Exported wrappers used by CLI without exposing internals elsewhere
func SetSourceFSForCLI(fsys fs.FS) { setSourceFS(fsys) }
func ClearSourceFSForCLI()         { clearSourceFS() }

func setSourceFS(fsys fs.FS) { sourceFSHolder.fs = fsys }
func clearSourceFS()         { sourceFSHolder.fs = nil }
func getSourceFS() fs.FS     { return sourceFSHolder.fs }

// GetOrCreateSSHPool returns an existing SSH pool or creates a new one
func (c *HostContext) GetOrCreateSSHPool() (*desopssshpool.Pool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sshPoolManager == nil {
		// Create SSH pool manager if it doesn't exist
		c.sshPoolManager = sshpool.NewManager(c.Host.Config)
	}

	// Get or create pool for this host
	pool, err := c.sshPoolManager.GetPool(c.Host.Host, c.Host.Vars)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH pool for host %s: %w", c.Host.Host, err)
	}

	return pool, nil
}

// GetOrCreateSftpClient returns an existing SFTP client or creates a new one using the SSH pool
func (c *HostContext) GetOrCreateSftpClient() (*runtime.SftpClient, error) {
	// Get SSH pool
	pool, err := c.GetOrCreateSSHPool()
	if err != nil {
		return nil, err
	}

	// Get SFTP session from pool
	sftpSession, err := pool.GetSFTP(c.Host.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to get SFTP session from pool for host %s: %w", c.Host.Host, err)
	}

	// Create SftpClient wrapper that works with the sshpool SFTPSession
	// Note: The sshpool SFTPSession embeds *sftp.Client, so we can access it directly
	// For now, we'll create a minimal SftpClient without the sshClient field
	// since it's only used for error reporting and we can get the host info from context
	sftpClient := &runtime.SftpClient{
		Client: sftpSession.Client, // Access the embedded sftp.Client
	}

	return sftpClient, nil
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
		_, err := hc.GetOrCreateSSHPool()
		if err != nil {
			return nil, err
		}
	}

	return hc, nil
}

func ReadTemplateFile(filename string) (string, error) {
	// If a source FS is configured, read from it using templates/ relative path semantics
	if fsys := getSourceFS(); fsys != nil {
		// Module file sources are typically relative to a templates dir when not absolute
		rel := filename
		if !filepath.IsAbs(filename) {
			rel = filepath.Join("templates", filename)
		}
		// Convert to slash for fs.FS
		b, err := fs.ReadFile(fsys, filepath.ToSlash(rel))
		if err != nil {
			return "", fmt.Errorf("failed to read file %s from FS: %w", rel, err)
		}
		return string(b), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}
	common.LogDebug("Reading template file in templates directory", map[string]interface{}{"filename": filename, "dir": filepath.Dir(filename), "cwd": cwd})
	if !filepath.IsAbs(filename) {
		return ReadLocalFile(filepath.Join("templates", filename))
	}
	return ReadLocalFile(filename)
}

// ReadSourceFile reads a file that may be part of the source bundle FS (if configured),
// falling back to the local filesystem.
func ReadSourceFile(filename string) (string, error) {
	if fsys := getSourceFS(); fsys != nil {
		b, err := fs.ReadFile(fsys, filepath.ToSlash(filename))
		if err != nil {
			return "", fmt.Errorf("failed to read file %s from FS: %w", filename, err)
		}
		return string(b), nil
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
	return runtime.ReadRemoteFileBytes(sftpClient, filename)
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
	return runtime.WriteRemoteFile(sftpClient, filename, contents)
}

func (c *HostContext) Copy(src, dst string) error {
	if c.Host.IsLocal {
		return runtime.CopyLocal(src, dst)
	}

	sftpClient, err := c.GetOrCreateSftpClient()
	if err != nil {
		return fmt.Errorf("failed to get SFTP client for remote host %s: %w", c.Host.Host, err)
	}
	return runtime.CopyRemote(sftpClient, src, dst)
}

func (c *HostContext) SetFileMode(path, mode, username string) error {
	if c.Host.IsLocal {
		return runtime.SetLocalFileMode(path, mode)
	}

	sftpClient, err := c.GetOrCreateSftpClient()
	if err != nil {
		return fmt.Errorf("failed to get SFTP client for remote host %s: %w", c.Host.Host, err)
	}
	return runtime.SetRemoteFileMode(sftpClient, path, mode)
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
	return runtime.StatRemote(sftpClient, path)
}

func (c *HostContext) RunCommand(command, username string) (int, string, string, error) {
	return c.RunCommandWithShell(command, username, false)
}

func (c *HostContext) RunCommandWithShell(command, username string, useShell bool) (int, string, string, error) {
	if c.Host.IsLocal {
		return runtime.RunLocalCommandWithShell(command, username, useShell, c.Host.Config)
	}

	// Get SSH pool
	pool, err := c.GetOrCreateSSHPool()
	if err != nil {
		return -1, "", "", fmt.Errorf("failed to get SSH pool for remote host %s: %w", c.Host.Host, err)
	}

	return runtime.RunRemoteCommandWithShell(pool, c.Host.Host, command, username, c.Host.Config, useShell)
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

	if c.sshPoolManager != nil {
		return c.sshPoolManager.Close()
	}
	return nil
}
