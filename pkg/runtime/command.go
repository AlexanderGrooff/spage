package runtime

import (
	"bytes"
	"fmt"
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

	// Combine all commands into a single script
	var scriptBuilder strings.Builder
	for i, command := range commands {
		if i > 0 {
			scriptBuilder.WriteString(" && ")
		}
		scriptBuilder.WriteString(fmt.Sprintf("echo '=== COMMAND %d ===' && %s", i+1, command))
	}

	script := scriptBuilder.String()
	var cmdToRun string
	if username != "" {
		cmdToRun = fmt.Sprintf("sudo -u %s bash -c '%s'", username, script)
	} else {
		cmdToRun = script
	}

	common.DebugOutput("Running batch commands on %s: %s", b.host, cmdToRun)

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(cmdToRun)
	rc := 0
	if err != nil {
		if exitError, ok := err.(*ssh.ExitError); ok {
			rc = exitError.ExitStatus()
		} else {
			rc = -1
		}

		// Check if the error is due to sudo asking for password
		stderrOutput := stderr.String()
		if strings.Contains(stderrOutput, "[sudo] password") || strings.Contains(stderrOutput, "sudo: no tty present") || strings.Contains(stderrOutput, "sudo: no password was provided") {
			return nil, fmt.Errorf("sudo requires password input but SSH session is non-interactive. Consider: 1) Setting up passwordless sudo, 2) Using SSH key authentication, or 3) Running without username parameter")
		}
	}

	// Parse the output to separate individual command results
	output := stdout.String()
	stderrOutput := stderr.String()

	// For now, return a single result for the batch
	// In a more sophisticated implementation, we could parse the output to separate individual results
	return []CommandResult{
		{
			Command:  strings.Join(commands, " && "),
			ExitCode: rc,
			Stdout:   output,
			Stderr:   stderrOutput,
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

	// Request PTY for interactive session
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // enable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	// Request pseudo-terminal
	if err := session.RequestPty("xterm", 40, 80, modes); err != nil {
		return nil, fmt.Errorf("failed to request PTY for host %s: %w", b.host, err)
	}

	// Combine all commands into a single script
	var scriptBuilder strings.Builder
	for i, command := range commands {
		if i > 0 {
			scriptBuilder.WriteString(" && ")
		}
		scriptBuilder.WriteString(fmt.Sprintf("echo '=== COMMAND %d ===' && %s", i+1, command))
	}

	script := scriptBuilder.String()
	cmdToRun := fmt.Sprintf("sudo -Su %s bash -c '%s'", username, script)

	common.DebugOutput("Running interactive batch commands on %s: %s", b.host, cmdToRun)

	// Set up pipes for stdin, stdout, stderr
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// Start the command
	if err := session.Start(cmdToRun); err != nil {
		return nil, fmt.Errorf("failed to start interactive batch command on host %s: %w", b.host, err)
	}

	// Wait for the command to complete with a timeout
	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	// Wait for completion or timeout
	select {
	case err := <-done:
		rc := 0
		if err != nil {
			if exitError, ok := err.(*ssh.ExitError); ok {
				rc = exitError.ExitStatus()
			} else {
				rc = -1
			}
		}

		// Parse the output to separate individual command results
		output := stdout.String()
		stderrOutput := stderr.String()

		// For now, return a single result for the batch
		// In a more sophisticated implementation, we could parse the output to separate individual results
		return []CommandResult{
			{
				Command:  strings.Join(commands, " && "),
				ExitCode: rc,
				Stdout:   output,
				Stderr:   stderrOutput,
				Error:    err,
			},
		}, nil
	case <-time.After(30 * time.Second): // 30 second timeout
		err := session.Close()
		if err != nil {
			common.DebugOutput("Error closing session: %v", err)
		}
		return nil, fmt.Errorf("interactive batch command timed out on host %s", b.host)
	}
}

// CommandResult represents the result of a command execution
type CommandResult struct {
	Command  string
	ExitCode int
	Stdout   string
	Stderr   string
	Error    error
}

// RunInteractiveRemoteCommand executes a command on a remote host using SSH with PTY support
func RunInteractiveRemoteCommand(pool *desopssshpool.Pool, host, command, username string, cfg *config.Config) (int, string, string, error) {
	// Get a session from the pool
	session, err := pool.Get(host)
	if err != nil {
		return -1, "", "", fmt.Errorf("failed to get SSH session from pool for host %s: %w", host, err)
	}
	defer session.Put() // Important: return session to pool

	// Request PTY for interactive session
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // enable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	// Request pseudo-terminal
	if err := session.RequestPty("xterm", 40, 80, modes); err != nil {
		return -1, "", "", fmt.Errorf("failed to request PTY for host %s: %w", host, err)
	}

	var cmdToRun string
	if username != "" {
		// Use sudo with bash -c, single-quoting the command to preserve its structure
		cmdToRun = fmt.Sprintf("sudo -Su %s bash -c '%s'", username, command)
	} else {
		// Pass the command directly to session.Run without sh -c wrapper
		cmdToRun = command
	}

	common.DebugOutput("Running interactive remote command on %s: %s", host, cmdToRun)

	// Set up pipes for stdin, stdout, stderr
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// Start the command
	if err := session.Start(cmdToRun); err != nil {
		return -1, "", "", fmt.Errorf("failed to start interactive command on host %s: %w", host, err)
	}

	// Wait for the command to complete with a timeout
	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	// Wait for completion or timeout
	select {
	case err := <-done:
		rc := 0
		if err != nil {
			if exitError, ok := err.(*ssh.ExitError); ok {
				rc = exitError.ExitStatus()
			} else {
				rc = -1
			}
		}
		return rc, stdout.String(), stderr.String(), err
	case <-time.After(30 * time.Second): // 30 second timeout
		err := session.Close()
		if err != nil {
			common.DebugOutput("Error closing session: %v", err)
		}
		return -1, stdout.String(), stderr.String(), fmt.Errorf("interactive command timed out on host %s", host)
	}
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

	// Once a Session is created, you can execute a single command on
	// the remote side using the Run method.
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	var cmdToRun string
	if username != "" {
		// Use sudo with bash -c, single-quoting the command to preserve its structure
		cmdToRun = fmt.Sprintf("sudo -u %s bash -c '%s'", username, command)
	} else {
		// Pass the command directly to session.Run without sh -c wrapper
		cmdToRun = command
	}

	common.DebugOutput("Running remote command on %s: %s", host, cmdToRun)
	err = session.Run(cmdToRun)
	rc := 0
	if err != nil {
		if exitError, ok := err.(*ssh.ExitError); ok {
			rc = exitError.ExitStatus()
		} else {
			rc = -1 // Indicate a non-exit-related error
		}

		// Check if the error is due to sudo asking for password
		stderrOutput := stderr.String()
		if strings.Contains(stderrOutput, "[sudo] password") || strings.Contains(stderrOutput, "sudo: no tty present") || strings.Contains(stderrOutput, "sudo: no password was provided") {
			return rc, stdout.String(), stderrOutput, fmt.Errorf("sudo requires password input but SSH session is non-interactive on host %s. Consider: 1) Setting up passwordless sudo, 2) Using SSH key authentication, or 3) Running without username parameter", host)
		}

		// Include more context in the error message
		return rc, stdout.String(), stderrOutput, fmt.Errorf("failed to run remote command '%s' (original: '%s') on host %s: %w, stderr: %s", cmdToRun, command, host, err, stderrOutput)
	}
	return rc, stdout.String(), stderr.String(), nil
}
