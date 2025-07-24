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

func RunLocalCommand(command, username string) (int, string, string, error) {
	if command == "" {
		return 0, "", "", nil
	}
	var stdout, stderr bytes.Buffer
	var err error
	var cmd *exec.Cmd
	var cmdToSplit string
	if username != "" {
		cmdToSplit = fmt.Sprintf("sudo -Su %s %s", username, command)
	} else {
		cmdToSplit = command
	}
	splitCmd, err := shlex.Split(cmdToSplit)
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
	common.DebugOutput("Running command: %s", cmd.String())
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	rc := 0
	if err != nil {
		// Try to get the exit code
		if exitError, ok := err.(*exec.ExitError); ok {
			rc = exitError.ExitCode()
		} else {
			rc = -1 // Indicate a non-exit error (e.g., command not found)
		}
		return rc, stdout.String(), stderr.String(), fmt.Errorf("failed to execute command %q: %v", cmd.String(), err)
	}

	return rc, stdout.String(), stderr.String(), nil
}

// CommandResult represents the result of a command execution
type CommandResult struct {
	Command  string
	ExitCode int
	Stdout   string
	Stderr   string
	Error    error
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
		ssh.ECHO:          1,     // enable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	return ce.session.RequestPty("xterm", 40, 80, modes)
}

// executeCommand runs a command and returns the result
func (ce *commandExecutor) executeCommand(cmdToRun string, interactive bool) (int, string, string, error) {
	var stdout, stderr bytes.Buffer
	var err error
	var rc int

	if interactive {
		// For interactive commands, stream stdout and stderr in real-time
		// Add host prefix to distinguish remote output
		hostPrefix := fmt.Sprintf("[%s] ", ce.host)
		ce.session.Stdout = io.MultiWriter(&stdout, &hostWriter{os.Stdout, hostPrefix})
		ce.session.Stderr = io.MultiWriter(&stderr, &hostWriter{os.Stderr, hostPrefix})
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
		case <-time.After(30 * time.Second): // 30 second timeout
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

	return rc, stdout.String(), stderr.String(), err
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

// buildSudoCommand builds the appropriate sudo command based on configuration
func buildSudoCommand(command, username string, interactive bool) string {
	if username == "" {
		return command
	}

	if interactive {
		return fmt.Sprintf("sudo -Su %s bash -c '%s'", username, command)
	}
	return fmt.Sprintf("sudo -u %s bash -c '%s'", username, command)
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

// RunInteractiveRemoteCommand executes a command on a remote host using SSH with PTY support
func RunInteractiveRemoteCommand(pool *desopssshpool.Pool, host, command, username string, cfg *config.Config) (int, string, string, error) {
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
	cmdToRun := buildSudoCommand(command, username, true)
	common.DebugOutput("Running interactive remote command on %s: %s", host, cmdToRun)

	// Execute the command
	rc, stdout, stderr, err := executor.executeCommand(cmdToRun, true)
	return rc, stdout, stderr, err
}

// RunRemoteCommand executes a command on a remote host using SSH pool
func RunRemoteCommand(pool *desopssshpool.Pool, host, command, username string, cfg *config.Config) (int, string, string, error) {
	// If interactive mode is enabled and username is provided, use interactive execution
	if cfg != nil && cfg.Sudo.UseInteractive && username != "" {
		return RunInteractiveRemoteCommand(pool, host, command, username, cfg)
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
	cmdToRun := buildSudoCommand(command, username, false)
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
	cmdToRun := buildSudoCommand(script, username, false)

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
	cmdToRun := buildSudoCommand(script, username, true)

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
