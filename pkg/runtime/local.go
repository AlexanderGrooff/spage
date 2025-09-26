package runtime

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/google/shlex"
)

type LocalConnection struct {
}

func NewLocalConnection() *LocalConnection {
	return &LocalConnection{}
}

func (lc *LocalConnection) Close() error {
	return nil
}

// executeLocalCommand executes a command locally
func (lc *LocalConnection) ExecuteCommand(command string, opts *CommandOptions) (*CommandResult, error) {
	if command == "" {
		return nil, fmt.Errorf("command is empty")
	}

	cmdToRun := buildCommand(command, opts)
	var stdout, stderr bytes.Buffer
	var cmd *exec.Cmd

	splitCmd, err := shlex.Split(cmdToRun)
	if err != nil {
		return nil, fmt.Errorf("failed to split command %s: %v", command, err)
	}
	prog := splitCmd[0]
	args := splitCmd[1:]
	absProg, err := exec.LookPath(prog)
	if err != nil {
		return nil, fmt.Errorf("failed to find %s in $PATH: %v", prog, err)
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
		// Clean sudo prompts from output before returning error
		cleanedStdout := cleanSudoPrompts(stdout.String())
		cleanedStderr := cleanSudoPrompts(stderr.String())
		return NewCommandResult(cmd.String(), rc, cleanedStdout, cleanedStderr, fmt.Errorf("failed to execute command %q: %v", cmd.String(), err)), nil
	}

	// Clean sudo prompts from output before returning
	cleanedStdout := cleanSudoPrompts(stdout.String())
	cleanedStderr := cleanSudoPrompts(stderr.String())
	return NewCommandResult(cmd.String(), rc, cleanedStdout, cleanedStderr, nil), nil
}

// StatLocal retrieves local file information. If follow is true, it follows symlinks (os.Stat).
// If follow is false, it stats the link itself (os.Lstat).
func (c *LocalConnection) Stat(path string, follow bool) (os.FileInfo, error) {
	if follow {
		return os.Stat(path)
	} else {
		return os.Lstat(path)
	}
}

func (c *LocalConnection) SetFileMode(path, modeStr string) error {
	mode, err := parseFileMode(modeStr)
	if err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func (c *LocalConnection) ReadFile(filename string) ([]byte, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %w", err)
		}
		return nil, fmt.Errorf("failed to read local file %s: %w", filename, err)
	}
	return data, nil
}

func (c *LocalConnection) WriteFile(filename string, data string) error {
	return os.WriteFile(filename, []byte(data), 0644)
}

func (c *LocalConnection) CopyFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		return copyLocalDir(src, dst)
	}
	return copyLocalFile(src, dst)
}

func copyLocalDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err = os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err = copyLocalDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err = copyLocalFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyLocalFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := srcFile.Close(); err != nil {
			common.LogWarn("Failed to close source file", map[string]interface{}{
				"file":  src,
				"error": err.Error(),
			})
		}
	}()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer func() {
		if err := dstFile.Close(); err != nil {
			common.LogWarn("Failed to close destination file", map[string]interface{}{
				"file":  dst,
				"error": err.Error(),
			})
		}
	}()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
