package runtime

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
	desopssshpool "github.com/desops/sshpool"
	"github.com/google/shlex"
	"golang.org/x/crypto/ssh"
)

// CommandResult represents the result of a command execution
type CommandResult struct {
	Command  string
	ExitCode int
	Stdout   string
	Stderr   string
	Error    error
}

// CommandOptions holds configuration for command execution
type CommandOptions struct {
	Username        string
	UseShell        bool
	Interactive     bool
	UseSudo         bool
	SudoUser        string
	InteractiveSudo bool
}

// NewCommandOptions creates a new CommandOptions with sensible defaults
func NewCommandOptions() *CommandOptions {
	return &CommandOptions{
		UseShell:        false,
		Interactive:     false,
		UseSudo:         false,
		InteractiveSudo: false,
	}
}

// WithUsername sets the username for the command
func (co *CommandOptions) WithUsername(username string) *CommandOptions {
	co.Username = username
	co.UseSudo = username != ""
	return co
}

// WithShell enables shell execution
func (co *CommandOptions) WithShell() *CommandOptions {
	co.UseShell = true
	return co
}

// WithInteractive enables interactive execution
func (co *CommandOptions) WithInteractive() *CommandOptions {
	co.Interactive = true
	return co
}

// WithInteractiveSudo enables interactive sudo
func (co *CommandOptions) WithInteractiveSudo() *CommandOptions {
	co.InteractiveSudo = true
	return co
}

// escapeShellCommand properly escapes a command for use within bash -c '...'
func escapeShellCommand(command string) string {
	// Normalize line endings to Unix format
	escapedCmd := strings.ReplaceAll(command, "\r\n", "\n")

	// Replace backticks with $(...) syntax for better compatibility
	// This handles the case where backticks are used for command substitution
	var result strings.Builder
	var inBackticks bool
	var backtickContent strings.Builder

	for i := 0; i < len(escapedCmd); i++ {
		char := escapedCmd[i]
		if char == '`' {
			if inBackticks {
				// End of backtick section, convert to $()
				result.WriteString("$(")
				result.WriteString(backtickContent.String())
				result.WriteString(")")
				backtickContent.Reset()
				inBackticks = false
			} else {
				// Start of backtick section
				inBackticks = true
				backtickContent.Reset()
			}
		} else if inBackticks {
			backtickContent.WriteByte(char)
		} else {
			result.WriteByte(char)
		}
	}

	// If we have unclosed backticks, just append the content as-is
	if inBackticks {
		result.WriteString("`")
		result.WriteString(backtickContent.String())
	}

	escapedCmd = result.String()

	// Then escape single quotes by replacing ' with '\''
	escapedCmd = strings.ReplaceAll(escapedCmd, "'", "'\\''")

	return escapedCmd
}

// buildCommand constructs the final command string based on options
func buildCommand(command string, opts *CommandOptions) string {
	if command == "" {
		return ""
	}

	// Apply shell escaping if needed
	if opts.UseShell {
		command = escapeShellCommand(command)
	}

	// Build the command based on options
	if !opts.UseSudo {
		if opts.UseShell {
			return fmt.Sprintf("bash -c '%s'", command)
		}
		return command
	}

	// Handle sudo commands
	if opts.InteractiveSudo {
		if opts.UseShell {
			return fmt.Sprintf("sudo -Su %s bash -c '%s'", opts.Username, command)
		}
		return fmt.Sprintf("sudo -Su %s %s", opts.Username, command)
	}

	if opts.UseShell {
		return fmt.Sprintf("sudo -u %s bash -c '%s'", opts.Username, command)
	}
	return fmt.Sprintf("sudo -u %s %s", opts.Username, command)
}

// executeLocalCommand executes a command locally
func executeLocalCommand(command string, opts *CommandOptions) (int, string, string, error) {
	if command == "" {
		return 0, "", "", nil
	}

	cmdToRun := buildCommand(command, opts)
	var stdout, stderr bytes.Buffer
	var cmd *exec.Cmd

	if opts.UseShell {
		// For shell commands, execute directly through the shell
		cmd = exec.Command("bash", "-c", cmdToRun)
	} else {
		// For non-shell commands, use shlex.Split for proper argument parsing
		splitCmd, err := shlex.Split(cmdToRun)
		if err != nil {
			return -1, "", "", fmt.Errorf("failed to split command %s: %v", command, err)
		}
		prog := splitCmd[0]
		args := splitCmd[1:]
		absProg, err := exec.LookPath(prog)
		if err != nil {
			return -1, "", "", fmt.Errorf("failed to find %s in $PATH: %v", prog, err)
		}
		cmd = exec.Command(absProg, args...)
	}

	common.DebugOutput("Running command: %s", cmd.String())
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	rc := 0
	if err != nil {
		// Try to get the exit code
		if exitError, ok := err.(*exec.ExitError); ok {
			rc = exitError.ExitCode()
		} else {
			rc = -1 // Indicate a non-exit error (e.g., command not found)
		}
		// Clean sudo prompts from output before returning error
		cleanedStdout := cleanSudoPrompts(stdout.String())
		cleanedStderr := cleanSudoPrompts(stderr.String())
		return rc, cleanedStdout, cleanedStderr, fmt.Errorf("failed to execute command %q: %v", cmd.String(), err)
	}

	// Clean sudo prompts from output before returning
	cleanedStdout := cleanSudoPrompts(stdout.String())
	cleanedStderr := cleanSudoPrompts(stderr.String())
	return rc, cleanedStdout, cleanedStderr, nil
}

// hostWriter adds a host prefix to output for interactive commands
type hostWriter struct {
	writer     io.Writer
	hostPrefix string
}

func (hw *hostWriter) Write(p []byte) (n int, err error) {
	// Write the host prefix first
	if _, err := hw.writer.Write([]byte(hw.hostPrefix)); err != nil {
		return 0, err
	}
	// Then write the actual data
	return hw.writer.Write(p)
}

// commandExecutor handles the execution of commands with different modes
type commandExecutor struct {
	session *desopssshpool.Session
	host    string
}

// newCommandExecutor creates a new command executor
func newCommandExecutor(session *desopssshpool.Session, host string) *commandExecutor {
	return &commandExecutor{
		session: session,
		host:    host,
	}
}

// setupPTY configures a pseudo-terminal for interactive sessions
func (ce *commandExecutor) setupPTY() error {
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // enable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	return ce.session.RequestPty("xterm", 40, 80, modes)
}

// cleanSudoPrompts removes sudo password prompts from the output
func cleanSudoPrompts(output string) string {
	lines := strings.Split(output, "\n")
	var cleanedLines []string

	for _, line := range lines {
		// Skip lines that match sudo password prompt patterns
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "[sudo] password for ") ||
			strings.HasPrefix(trimmedLine, "sudo: no tty present") ||
			strings.HasPrefix(trimmedLine, "sudo: no password was provided") ||
			strings.Contains(trimmedLine, "password:") {
			continue
		}
		cleanedLines = append(cleanedLines, line)
	}

	return strings.Join(cleanedLines, "\n")
}

// executeCommand runs a command and returns the result
func (ce *commandExecutor) executeCommand(cmdToRun string, interactive bool) (int, string, string, error) {
	var stdout, stderr bytes.Buffer
	var err error
	var rc int

	if interactive {
		// For interactive commands, stream stdout and stderr in real-time
		// Add host prefix to distinguish remote output
		hostPrefix := "" // fmt.Sprintf("[%s] ", ce.host)
		ce.session.Stdout = io.MultiWriter(&stdout, &hostWriter{os.Stdout, hostPrefix})
		ce.session.Stderr = io.MultiWriter(&stderr, &hostWriter{os.Stderr, hostPrefix})

		// Set up stdin to stream from user's console
		ce.session.Stdin = os.Stdin
	} else {
		// For non-interactive commands, buffer everything
		ce.session.Stdout = &stdout
		ce.session.Stderr = &stderr
	}

	if interactive {
		// Start the command for interactive execution
		if err = ce.session.Start(cmdToRun); err != nil {
			return -1, "", "", fmt.Errorf("failed to start interactive command on host %s: %w", ce.host, err)
		}

		// Wait for the command to complete with a timeout
		done := make(chan error, 1)
		go func() {
			done <- ce.session.Wait()
		}()

		// Wait for completion or timeout
		select {
		case err = <-done:
			rc = ce.getExitCode(err)
		case <-time.After(10 * time.Minute): // 10 minute timeout
			closeErr := ce.session.Close()
			if closeErr != nil {
				common.DebugOutput("Error closing session: %v", closeErr)
			}
			return -1, stdout.String(), stderr.String(), fmt.Errorf("interactive command timed out on host %s", ce.host)
		}
	} else {
		// Run the command directly for non-interactive execution
		err = ce.session.Run(cmdToRun)
		rc = ce.getExitCode(err)
	}

	// Clean sudo prompts from stdout before returning
	cleanedStdout := cleanSudoPrompts(stdout.String())
	cleanedStderr := cleanSudoPrompts(stderr.String())

	return rc, cleanedStdout, cleanedStderr, err
}

// getExitCode extracts the exit code from an error
func (ce *commandExecutor) getExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitError, ok := err.(*ssh.ExitError); ok {
		return exitError.ExitStatus()
	}
	return -1
}

// executeRemoteCommand executes a command on a remote host
func executeRemoteCommand(pool *desopssshpool.Pool, host, command string, opts *CommandOptions, cfg *config.Config) (int, string, string, error) {
	// If interactive mode is enabled and username is provided, use interactive execution
	if cfg != nil && cfg.Sudo.UseInteractive && opts.UseSudo {
		return executeInteractiveRemoteCommand(pool, host, command, opts, cfg)
	}

	// Get a session from the pool
	session, err := pool.Get(host)
	if err != nil {
		return -1, "", "", fmt.Errorf("failed to get SSH session from pool for host %s: %w", host, err)
	}
	defer session.Put() // Important: return session to pool

	// Create command executor
	executor := newCommandExecutor(session, host)

	// Build the command
	cmdToRun := buildCommand(command, opts)
	common.DebugOutput("Running remote command on %s: %s", host, cmdToRun)

	// Execute the command
	rc, stdout, stderr, err := executor.executeCommand(cmdToRun, false)

	// Check for sudo password errors
	if err != nil {
		if sudoErr := checkSudoPasswordError(stderr, host); sudoErr != nil {
			return rc, stdout, stderr, sudoErr
		}
		// Include more context in the error message
		return rc, stdout, stderr, fmt.Errorf("failed to run remote command '%s' (original: '%s') on host %s: %w, stderr: %s", cmdToRun, command, host, err, stderr)
	}

	return rc, stdout, stderr, nil
}

// executeInteractiveRemoteCommand executes an interactive command on a remote host
func executeInteractiveRemoteCommand(pool *desopssshpool.Pool, host, command string, opts *CommandOptions, cfg *config.Config) (int, string, string, error) {
	// Get a session from the pool
	session, err := pool.Get(host)
	if err != nil {
		return -1, "", "", fmt.Errorf("failed to get SSH session from pool for host %s: %w", host, err)
	}
	defer session.Put() // Important: return session to pool

	// Create command executor
	executor := newCommandExecutor(session, host)

	// Setup PTY for interactive session
	if err := executor.setupPTY(); err != nil {
		return -1, "", "", fmt.Errorf("failed to request PTY for host %s: %w", host, err)
	}

	// Build the command
	cmdToRun := buildCommand(command, opts)
	common.DebugOutput("Running interactive remote command on %s: %s", host, cmdToRun)

	// Execute the command
	rc, stdout, stderr, err := executor.executeCommand(cmdToRun, true)
	return rc, stdout, stderr, err
}

// checkSudoPasswordError checks if the error is due to sudo asking for password
func checkSudoPasswordError(stderrOutput, host string) error {
	if strings.Contains(stderrOutput, "[sudo] password") ||
		strings.Contains(stderrOutput, "sudo: no tty present") ||
		strings.Contains(stderrOutput, "sudo: no password was provided") {
		return fmt.Errorf("sudo requires password input but SSH session is non-interactive on host %s. Consider: 1) Setting up passwordless sudo, 2) Using SSH key authentication, or 3) Running without username parameter", host)
	}
	return nil
}

// Public API functions - these maintain backward compatibility

func RunLocalCommand(command, username string) (int, string, string, error) {
	opts := NewCommandOptions()
	if username != "" {
		opts.WithUsername(username)
	}
	return executeLocalCommand(command, opts)
}

func RunLocalCommandWithShell(command, username string, useShell bool) (int, string, string, error) {
	opts := NewCommandOptions()
	if username != "" {
		opts.WithUsername(username)
	}
	if useShell {
		opts.WithShell()
	}
	return executeLocalCommand(command, opts)
}

func RunRemoteCommand(pool *desopssshpool.Pool, host, command, username string, cfg *config.Config) (int, string, string, error) {
	opts := NewCommandOptions()
	if username != "" {
		opts.WithUsername(username)
	}
	return executeRemoteCommand(pool, host, command, opts, cfg)
}

func RunRemoteCommandWithShell(pool *desopssshpool.Pool, host, command, username string, cfg *config.Config, useShell bool) (int, string, string, error) {
	opts := NewCommandOptions()
	if username != "" {
		opts.WithUsername(username)
	}
	if useShell {
		opts.WithShell()
	}
	return executeRemoteCommand(pool, host, command, opts, cfg)
}

func RunInteractiveRemoteCommand(pool *desopssshpool.Pool, host, command, username string, cfg *config.Config) (int, string, string, error) {
	opts := NewCommandOptions()
	if username != "" {
		opts.WithUsername(username).WithInteractiveSudo()
	}
	opts.WithInteractive()
	return executeInteractiveRemoteCommand(pool, host, command, opts, cfg)
}

// BatchCommandExecutor allows executing multiple commands in a single SSH session
type BatchCommandExecutor struct {
	pool *desopssshpool.Pool
	host string
	cfg  *config.Config
}

// NewBatchCommandExecutor creates a new batch command executor using SSH pool
func NewBatchCommandExecutor(pool *desopssshpool.Pool, host string, cfg *config.Config) *BatchCommandExecutor {
	return &BatchCommandExecutor{
		pool: pool,
		host: host,
		cfg:  cfg,
	}
}

// buildBatchScript combines multiple commands into a single script
func buildBatchScript(commands []string) string {
	var scriptBuilder strings.Builder
	for i, command := range commands {
		if i > 0 {
			scriptBuilder.WriteString(" && ")
		}
		scriptBuilder.WriteString(fmt.Sprintf("echo '=== COMMAND %d ===' && %s", i+1, command))
	}
	return scriptBuilder.String()
}

// ExecuteBatch executes multiple commands in a single SSH session using the pool
func (b *BatchCommandExecutor) ExecuteBatch(commands []string, username string) ([]CommandResult, error) {
	if len(commands) == 0 {
		return nil, nil
	}

	// If interactive mode is enabled and username is provided, use interactive execution
	if b.cfg != nil && b.cfg.Sudo.UseInteractive && username != "" {
		return b.executeInteractiveBatch(commands, username)
	}

	// Get a session from the pool
	session, err := b.pool.Get(b.host)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH session from pool for host %s: %w", b.host, err)
	}
	defer session.Put() // Important: return session to pool

	// Create command executor
	executor := newCommandExecutor(session, b.host)

	// Build the script and command
	script := buildBatchScript(commands)
	opts := NewCommandOptions()
	if username != "" {
		opts.WithUsername(username)
	}
	cmdToRun := buildCommand(script, opts)

	common.DebugOutput("Running batch commands on %s: %s", b.host, cmdToRun)

	// Execute the command
	rc, stdout, stderr, err := executor.executeCommand(cmdToRun, false)

	// Check for sudo password errors
	if err != nil {
		if sudoErr := checkSudoPasswordError(stderr, b.host); sudoErr != nil {
			return nil, sudoErr
		}
	}

	// Return a single result for the batch
	return []CommandResult{
		{
			Command:  strings.Join(commands, " && "),
			ExitCode: rc,
			Stdout:   stdout,
			Stderr:   stderr,
			Error:    err,
		},
	}, nil
}

// executeInteractiveBatch executes multiple commands in an interactive SSH session
func (b *BatchCommandExecutor) executeInteractiveBatch(commands []string, username string) ([]CommandResult, error) {
	// Get a session from the pool
	session, err := b.pool.Get(b.host)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH session from pool for host %s: %w", b.host, err)
	}
	defer session.Put() // Important: return session to pool

	// Create command executor
	executor := newCommandExecutor(session, b.host)

	// Setup PTY for interactive session
	if err := executor.setupPTY(); err != nil {
		return nil, fmt.Errorf("failed to request PTY for host %s: %w", b.host, err)
	}

	// Build the script and command
	script := buildBatchScript(commands)
	opts := NewCommandOptions()
	if username != "" {
		opts.WithUsername(username).WithInteractiveSudo()
	}
	opts.WithInteractive()
	cmdToRun := buildCommand(script, opts)

	common.DebugOutput("Running interactive batch commands on %s: %s", b.host, cmdToRun)

	// Execute the command
	rc, stdout, stderr, err := executor.executeCommand(cmdToRun, true)

	// Return a single result for the batch
	return []CommandResult{
		{
			Command:  strings.Join(commands, " && "),
			ExitCode: rc,
			Stdout:   stdout,
			Stderr:   stderr,
			Error:    err,
		},
	}, nil
}
