package pkg

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/AlexanderGrooff/jinja-go"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
	"github.com/AlexanderGrooff/spage/pkg/runtime"
)

// HostContext represents the context for a specific host during playbook execution.
// It contains host-specific data like facts, variables, and SSH connections.
type HostContext struct {
	Host            *Host
	Facts           *sync.Map
	History         *sync.Map
	HandlerTracker  *HandlerTracker
	Connection      runtime.Connection       // Connection to target machine
	LocalConnection *runtime.LocalConnection // For operations on the machine running ansible-playbook, like getting template files
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

func InitializeHostContext(host *Host, cfg *config.Config) (*HostContext, error) {
	hc := &HostContext{
		Host:           host,
		Facts:          new(sync.Map),
		History:        new(sync.Map),
		HandlerTracker: nil, // Will be initialized later with handlers
		// TODO: Default to local connection for now; remote connection wiring can be added when needed.
		Connection:      runtime.NewLocalConnection(),
		LocalConnection: runtime.NewLocalConnection(),
	}

	// Set config on host for later use
	host.Config = cfg

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

func (c *HostContext) ReadFile(filename string, _ string) (string, error) {
	bytes, err := c.Connection.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// ReadFileBytes reads a file and returns raw bytes. Username is currently ignored
// since the underlying runtime.Connection handles authentication/context.
func (c *HostContext) ReadFileBytes(filename string, _ string) ([]byte, error) {
	return c.Connection.ReadFile(filename)
}

func (c *HostContext) WriteFile(filename, contents, username string) error {
	// Delegate to configured connection implementation
	return c.Connection.WriteFile(filename, contents)
}

func (c *HostContext) Copy(src, dst string) error {
	// Delegate to configured connection implementation
	return c.Connection.CopyFile(src, dst)
}

func (c *HostContext) SetFileMode(path, mode, username string) error {
	return c.Connection.SetFileMode(path, mode)
}

func (c *HostContext) Stat(path string, follow bool) (os.FileInfo, error) {
	return c.Connection.Stat(path, follow)
}

func (c *HostContext) RunCommand(command, username string) (int, string, string, error) {
	return c.RunCommandWithShell(command, username, false)
}

func (c *HostContext) RunCommandWithShell(command, username string, useShell bool) (int, string, string, error) {
	var cfg *config.Config
	if c.Host.Config != nil {
		if hostCfg, ok := c.Host.Config.(*config.Config); ok {
			cfg = hostCfg
		}
	}
	opts := runtime.NewCommandOptions(cfg).WithUsername(username)
	if useShell {
		opts = opts.WithShell()
	}
	res, err := c.Connection.ExecuteCommand(command, opts)
	if err != nil {
		// Even on error, ExecuteCommand returns CommandResult with context; prefer that if available
		if res != nil {
			return res.ExitCode, res.Stdout, res.Stderr, res.Error
		}
		return -1, "", "", err
	}
	return res.ExitCode, res.Stdout, res.Stderr, res.Error
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

func GetVariableUsageFromTemplate(s string) []string {
	if CheckIfVariableIsUnused(s) {
		common.LogDebug("Skipping variable extraction for unused variable", map[string]interface{}{"variable": s})
		return nil
	}
	vars, err := jinja.ParseVariables(s)
	if err != nil {
		return nil
	}
	return vars
}

// InitializeHandlerTracker initializes the HandlerTracker with the provided handlers
func (c *HostContext) InitializeHandlerTracker(handlers []GraphNode) {
	if c.HandlerTracker == nil {
		c.HandlerTracker = NewHandlerTracker(c.Host.Name, handlers)
	}
}

func (c *HostContext) Close() error {
	if c.Connection != nil {
		return c.Connection.Close()
	}
	return nil
}
