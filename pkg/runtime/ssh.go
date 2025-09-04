package runtime

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
	"github.com/AlexanderGrooff/spage/pkg/sshpool"
	desopssshpool "github.com/desops/sshpool"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type SSHConnection struct {
	Host       string
	sshPool    *desopssshpool.Pool
	mu         sync.RWMutex
	cfg        *config.SSHConfig
	sftpClient *SftpClient
}

func NewSSHConnection(cfg *config.SSHConfig) (*SSHConnection, error) {
	c := &SSHConnection{
		mu:  sync.RWMutex{},
		cfg: cfg,
	}
	var err error
	c.sftpClient, err = c.GetOrCreateSftpClient()
	if err != nil {
		return nil, err
	}
	c.sshPool, err = c.GetOrCreateSSHPool()
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetOrCreateSSHPool returns an existing SSH pool or creates a new one
func (c *SSHConnection) GetOrCreateSSHPool() (*desopssshpool.Pool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sshPool != nil {
		return c.sshPool, nil
	}

	sshPoolManager := sshpool.NewManager(c.cfg)

	// Get or create pool for this host
	pool, err := sshPoolManager.GetPool(c.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH pool for host %s: %w", c.Host, err)
	}

	return pool, nil
}

// GetOrCreateSftpClient returns an existing SFTP client or creates a new one using the SSH pool
func (c *SSHConnection) GetOrCreateSftpClient() (*SftpClient, error) {
	// Get SSH pool
	pool, err := c.GetOrCreateSSHPool()
	if err != nil {
		return nil, err
	}

	// Get SFTP session from pool
	sftpSession, err := pool.GetSFTP(c.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to get SFTP session from pool for host %s: %w", c.Host, err)
	}

	// Create SftpClient wrapper that works with the sshpool SFTPSession
	// Note: The sshpool SFTPSession embeds *sftp.Client, so we can access it directly
	// For now, we'll create a minimal SftpClient without the sshClient field
	// since it's only used for error reporting and we can get the host info from context
	sftpClient := &SftpClient{
		Client: sftpSession.Client, // Access the embedded sftp.Client
	}

	return sftpClient, nil
}

// executeRemoteCommand executes a command on a remote host
func (c *SSHConnection) ExecuteCommand(command string, opts *CommandOptions) (*CommandResult, error) {
	// If interactive mode is enabled and username is provided, use interactive execution
	if opts.Interactive {
		return c.executeInteractiveRemoteCommand(command, opts)
	}

	// Get a session from the pool
	pool, err := c.GetOrCreateSSHPool()
	if err != nil {
		return nil, err
	}
	session, err := pool.Get(c.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH session from pool for host %s: %w", c.Host, err)
	}
	defer session.Put() // Important: return session to pool

	// Create command executor
	executor := newSSHCommandExecutor(session, c.Host)

	// Build the command
	cmdToRun := buildCommand(command, opts)
	common.DebugOutput("Running remote command on %s: %s", c.Host, cmdToRun)

	// Execute the command
	rc, stdout, stderr, err := executor.executeCommand(cmdToRun, false)

	// Check for authentication and sudo password errors
	if err != nil {
		if authErr := checkAuthenticationError(err, stderr, c.Host); authErr != nil {
			return NewCommandResult(cmdToRun, rc, stdout, stderr, authErr), nil
		}
		if sudoErr := checkSudoPasswordError(stderr, c.Host); sudoErr != nil {
			return NewCommandResult(cmdToRun, rc, stdout, stderr, sudoErr), nil
		}
		// Include more context in the error message
		return NewCommandResult(cmdToRun, rc, stdout, stderr, fmt.Errorf("failed to run remote command '%s' (original: '%s') on host %s: %w, stderr: %s", cmdToRun, command, c.Host, err, stderr)), nil
	}

	return NewCommandResult(cmdToRun, rc, stdout, stderr, nil), nil
}

func copyRemoteRecursive(sftpClient *sftp.Client, src, dst string) error {
	srcInfo, err := sftpClient.Lstat(src) // Use Lstat to handle symlinks correctly if needed later
	if err != nil {
		return fmt.Errorf("failed to stat remote source %s: %w", src, err)
	}

	if srcInfo.IsDir() {
		// Create destination directory
		if err := sftpClient.MkdirAll(dst); err != nil && !os.IsExist(err) {
			return fmt.Errorf("failed to create remote directory %s: %w", dst, err)
		}
		// Set permissions explicitly after creation/check
		if err := sftpClient.Chmod(dst, srcInfo.Mode().Perm()); err != nil { // Use Perm() for chmod
			return fmt.Errorf("failed to set mode on remote directory %s: %w", dst, err)
		}

		entries, err := sftpClient.ReadDir(src)
		if err != nil {
			return fmt.Errorf("failed to read remote directory %s: %w", src, err)
		}

		for _, entry := range entries {
			srcPath := sftpClient.Join(src, entry.Name()) // Use sftpClient.Join for remote paths
			dstPath := sftpClient.Join(dst, entry.Name())
			if err := copyRemoteRecursive(sftpClient, srcPath, dstPath); err != nil {
				return err // Propagate error up
			}
		}
	} else {
		// Copy file content
		srcFile, err := sftpClient.Open(src)
		if err != nil {
			return fmt.Errorf("failed to open remote source file %s: %w", src, err)
		}
		defer func() {
			if err := srcFile.Close(); err != nil {
				common.LogWarn("Failed to close remote source file", map[string]interface{}{
					"file":  src,
					"error": err.Error(),
				})
			}
		}()

		// Ensure destination directory exists before creating file
		dstDir := filepath.Dir(dst) // filepath.Dir should be okay here
		if err := sftpClient.MkdirAll(dstDir); err != nil && !os.IsExist(err) {
			return fmt.Errorf("failed to create remote directory %s for file %s: %w", dstDir, dst, err)
		}

		dstFile, err := sftpClient.Create(dst) // Create truncates if exists
		if err != nil {
			return fmt.Errorf("failed to create remote destination file %s: %w", dst, err)
		}
		defer func() {
			if err := dstFile.Close(); err != nil {
				common.LogWarn("Failed to close remote destination file", map[string]interface{}{
					"file":  dst,
					"error": err.Error(),
				})
			}
		}()

		if _, err := io.Copy(dstFile, srcFile); err != nil {
			return fmt.Errorf("failed to copy content from %s to %s: %w", src, dst, err)
		}
		// Set permissions after writing content
		if err := sftpClient.Chmod(dst, srcInfo.Mode().Perm()); err != nil { // Use Perm()
			return fmt.Errorf("failed to set mode on remote file %s: %w", dst, err)
		}
	}
	return nil
}

// WriteRemoteFile writes data to a remote file using a pooled SFTP client
func (c *SSHConnection) WriteFile(remotePath, data string) error {
	// Ensure the directory exists
	remoteDir := filepath.Dir(remotePath)
	if err := c.sftpClient.MkdirAll(remoteDir); err != nil {
		// Ignore if directory already exists, but return other errors
		if !os.IsExist(err) {
			return fmt.Errorf("failed to create remote directory %s on %s: %w", remoteDir, c.sftpClient.getHostInfo(), err)
		}
	}

	// Create or truncate the remote file
	f, err := c.sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("failed to create remote file %s on %s: %w", remotePath, c.sftpClient.getHostInfo(), err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			common.LogWarn("Failed to close remote file", map[string]interface{}{
				"file":  remotePath,
				"host":  c.sftpClient.getHostInfo(),
				"error": err.Error(),
			})
		}
	}()

	// Write the data
	if _, err := f.Write([]byte(data)); err != nil {
		return fmt.Errorf("failed to write data to remote file %s on %s: %w", remotePath, c.sftpClient.getHostInfo(), err)
	}

	return nil
}

// ReadRemoteFileBytes reads the content of a remote file as raw bytes using a pooled SFTP client
func (c *SSHConnection) ReadFile(remotePath string) ([]byte, error) {
	// Open the remote file
	f, err := c.sftpClient.Open(remotePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s on host %s", remotePath, c.sftpClient.getHostInfo())
		}
		return nil, fmt.Errorf("failed to open remote file %s on %s: %w", remotePath, c.sftpClient.getHostInfo(), err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			common.LogWarn("Failed to close remote file", map[string]interface{}{
				"file":  remotePath,
				"host":  c.sftpClient.getHostInfo(),
				"error": err.Error(),
			})
		}
	}()

	// Read all bytes from the file
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read data from remote file %s on %s: %w", remotePath, c.sftpClient.getHostInfo(), err)
	}

	return data, nil
}

// SetRemoteFileMode sets the mode of a remote file using a pooled SFTP client
func (c *SSHConnection) SetFileMode(path, modeStr string) error {
	mode, err := parseFileMode(modeStr)
	if err != nil {
		return err // Error parsing mode string
	}

	// Set the mode using SFTP
	err = c.sftpClient.Chmod(path, mode)
	if err != nil {
		return fmt.Errorf("failed to set mode %s (%o) on remote file %s on %s: %w", modeStr, mode, path, c.sftpClient.getHostInfo(), err)
	}
	return nil
}

// CopyRemote copies a file or directory recursively on the remote host using a pooled SFTP client
func (c *SSHConnection) CopyFile(src, dst string) error {
	return copyRemoteRecursive(c.sftpClient.Client, src, dst)
}

// checkAuthenticationError checks if the error is due to SSH authentication failure
func checkAuthenticationError(execErr error, stderrOutput, host string) error {
	if execErr == nil {
		return nil
	}

	// Check for common SSH authentication error patterns
	errorStr := execErr.Error()
	if strings.Contains(errorStr, "ssh: handshake failed") ||
		strings.Contains(errorStr, "ssh: unable to authenticate") ||
		strings.Contains(errorStr, "permission denied") ||
		strings.Contains(errorStr, "authentication failed") {
		return fmt.Errorf("SSH authentication failed for host %s. Consider: 1) Checking SSH key setup, 2) Verifying SSH agent is running, 3) Using interactive password authentication, or 4) Checking host connectivity: %w", host, execErr)
	}

	return nil
}

// executeInteractiveRemoteCommand executes an interactive command on a remote host
func (c *SSHConnection) executeInteractiveRemoteCommand(command string, opts *CommandOptions) (*CommandResult, error) {
	// Get a session from the pool
	pool, err := c.GetOrCreateSSHPool()
	if err != nil {
		return nil, err
	}
	session, err := pool.Get(c.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH session from pool for host %s: %w", c.Host, err)
	}
	defer session.Put() // Important: return session to pool

	// Create command executor
	executor := newSSHCommandExecutor(session, c.Host)

	// Setup PTY for interactive session
	if err := executor.setupPTY(); err != nil {
		return nil, fmt.Errorf("failed to request PTY for host %s: %w", c.Host, err)
	}

	// Build the command
	cmdToRun := buildCommand(command, opts)
	common.DebugOutput("Running interactive remote command on %s: %s", c.Host, cmdToRun)

	// Execute the command
	rc, stdout, stderr, err := executor.executeCommand(cmdToRun, true)
	return NewCommandResult(cmdToRun, rc, stdout, stderr, err), nil
}

// SSHCommandExecutor handles the execution of commands with different modes
type SSHCommandExecutor struct {
	session *desopssshpool.Session
	host    string
}

// newSSHCommandExecutor creates a new command executor
func newSSHCommandExecutor(session *desopssshpool.Session, host string) *SSHCommandExecutor {
	return &SSHCommandExecutor{
		session: session,
		host:    host,
	}
}

// setupPTY configures a pseudo-terminal for interactive sessions
func (ce *SSHCommandExecutor) setupPTY() error {
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // enable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	return ce.session.RequestPty("xterm", 40, 80, modes)
}

// executeCommand runs a command and returns the result
func (ce *SSHCommandExecutor) executeCommand(cmdToRun string, interactive bool) (int, string, string, error) {
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
func (ce *SSHCommandExecutor) getExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitError, ok := err.(*ssh.ExitError); ok {
		return exitError.ExitStatus()
	}
	return -1
}

// StatRemote retrieves remote file information using a pooled SFTP client
func (c *SSHConnection) Stat(path string, follow bool) (os.FileInfo, error) {
	// TODO: implement follow
	if follow {
		common.LogWarn("Stat with follow is not supported for remote hosts", map[string]interface{}{
			"host": c.Host,
			"path": path,
		})
	}
	return c.sftpClient.Lstat(path) // Use Lstat to handle symlinks correctly
}

func (c *SSHConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sshPool != nil {
		c.sshPool.Close()
	}
	return nil
}
