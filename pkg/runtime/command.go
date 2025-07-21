package runtime

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/common"
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
		cmdToSplit = fmt.Sprintf("sudo -u %s %s", username, command)
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
	client *ssh.Client
}

// NewBatchCommandExecutor creates a new batch command executor
func NewBatchCommandExecutor(client *ssh.Client) *BatchCommandExecutor {
	return &BatchCommandExecutor{client: client}
}

// ExecuteBatch executes multiple commands in a single SSH session
func (b *BatchCommandExecutor) ExecuteBatch(commands []string, username string) ([]CommandResult, error) {
	if len(commands) == 0 {
		return nil, nil
	}

	// Create a single session for all commands
	session, err := b.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create ssh session to %s: %w", b.client.RemoteAddr(), err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			common.LogWarn("Failed to close SSH session", map[string]interface{}{
				"host":  b.client.RemoteAddr().String(),
				"error": err.Error(),
			})
		}
	}()

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
		cmdToRun = fmt.Sprintf("sudo -u %s sh -c '%s'", username, script)
	} else {
		cmdToRun = script
	}

	common.DebugOutput("Running batch commands on %s: %s", b.client.RemoteAddr(), cmdToRun)

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

// CommandResult represents the result of a command execution
type CommandResult struct {
	Command  string
	ExitCode int
	Stdout   string
	Stderr   string
	Error    error
}

// RunRemoteCommand executes a command on a remote host using an existing SSH client connection.
func RunRemoteCommand(client *ssh.Client, command, username string) (int, string, string, error) {
	// Each ClientConn can support multiple interactive sessions,
	// represented by a Session. It's one session per command.
	session, err := client.NewSession()
	if err != nil {
		// Include the host address in the error if possible. client.RemoteAddr()
		return -1, "", "", fmt.Errorf("failed to create ssh session to %s: %w", client.RemoteAddr(), err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			common.LogWarn("Failed to close SSH session", map[string]interface{}{
				"host":  client.RemoteAddr().String(),
				"error": err.Error(),
			})
		}
	}()

	// Once a Session is created, you can execute a single command on
	// the remote side using the Run method.
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	var cmdToRun string
	if username != "" {
		// Use sudo with sh -c, single-quoting the command to preserve its structure
		cmdToRun = fmt.Sprintf("sudo -u %s sh -c '%s'", username, command)
	} else {
		// Pass the command directly to session.Run without sh -c wrapper
		cmdToRun = command
	}

	common.DebugOutput("Running remote command on %s: %s", client.RemoteAddr(), cmdToRun)
	err = session.Run(cmdToRun)
	rc := 0
	if err != nil {
		if exitError, ok := err.(*ssh.ExitError); ok {
			rc = exitError.ExitStatus()
		} else {
			rc = -1 // Indicate a non-exit-related error
		}
		// Include more context in the error message
		return rc, stdout.String(), stderr.String(), fmt.Errorf("failed to run remote command '%s' (original: '%s') on host %s: %w, stderr: %s", cmdToRun, command, client.RemoteAddr(), err, stderr.String())
	}
	return rc, stdout.String(), stderr.String(), nil
}
