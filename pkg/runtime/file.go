package runtime

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SftpClient wraps an SFTP client with SSH client information
type SftpClient struct {
	*sftp.Client
	sshClient *ssh.Client
}

// Close closes the SFTP client
func (s *SftpClient) Close() error {
	return s.Client.Close()
}

// getHostInfo returns a string representation of the host for error reporting
func (s *SftpClient) getHostInfo() string {
	if s.sshClient != nil && s.sshClient.RemoteAddr() != nil {
		return s.sshClient.RemoteAddr().String()
	}
	return "unknown"
}

// --- Local File Operations ---

func WriteLocalFile(filename string, data string) error {
	return os.WriteFile(filename, []byte(data), 0644)
}

func CopyLocal(src, dst string) error {
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

func ReadLocalFileBytes(filename string) ([]byte, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %w", err)
		}
		return nil, fmt.Errorf("failed to read local file %s: %w", filename, err)
	}
	return data, nil
}

// parseFileMode is used by both local and remote mode setting.
func parseFileMode(modeStr string) (os.FileMode, error) {
	// Try parsing as octal first
	if mode, err := strconv.ParseUint(modeStr, 8, 32); err == nil {
		return os.FileMode(mode), nil
	}
	// TODO: Add support for symbolic mode strings if needed (e.g., "u+x")
	return 0, fmt.Errorf("invalid file mode string: %q", modeStr)
}

func SetLocalFileMode(path, modeStr string) error {
	mode, err := parseFileMode(modeStr)
	if err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

// --- SFTP-based Remote File Operations ---

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
func WriteRemoteFile(sftpClient *SftpClient, remotePath, data string) error {
	// Ensure the directory exists
	remoteDir := filepath.Dir(remotePath)
	if err := sftpClient.MkdirAll(remoteDir); err != nil {
		// Ignore if directory already exists, but return other errors
		if !os.IsExist(err) {
			return fmt.Errorf("failed to create remote directory %s on %s: %w", remoteDir, sftpClient.getHostInfo(), err)
		}
	}

	// Create or truncate the remote file
	f, err := sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("failed to create remote file %s on %s: %w", remotePath, sftpClient.getHostInfo(), err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			common.LogWarn("Failed to close remote file", map[string]interface{}{
				"file":  remotePath,
				"host":  sftpClient.getHostInfo(),
				"error": err.Error(),
			})
		}
	}()

	// Write the data
	if _, err := f.Write([]byte(data)); err != nil {
		return fmt.Errorf("failed to write data to remote file %s on %s: %w", remotePath, sftpClient.getHostInfo(), err)
	}

	return nil
}

// ReadRemoteFileBytes reads the content of a remote file as raw bytes using a pooled SFTP client
func ReadRemoteFileBytes(sftpClient *SftpClient, remotePath string) ([]byte, error) {
	// Open the remote file
	f, err := sftpClient.Open(remotePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s on host %s", remotePath, sftpClient.getHostInfo())
		}
		return nil, fmt.Errorf("failed to open remote file %s on %s: %w", remotePath, sftpClient.getHostInfo(), err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			common.LogWarn("Failed to close remote file", map[string]interface{}{
				"file":  remotePath,
				"host":  sftpClient.getHostInfo(),
				"error": err.Error(),
			})
		}
	}()

	// Read all bytes from the file
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read data from remote file %s on %s: %w", remotePath, sftpClient.getHostInfo(), err)
	}

	return data, nil
}

// SetRemoteFileMode sets the mode of a remote file using a pooled SFTP client
func SetRemoteFileMode(sftpClient *SftpClient, path, modeStr string) error {
	mode, err := parseFileMode(modeStr)
	if err != nil {
		return err // Error parsing mode string
	}

	// Set the mode using SFTP
	err = sftpClient.Chmod(path, mode)
	if err != nil {
		return fmt.Errorf("failed to set mode %s (%o) on remote file %s on %s: %w", modeStr, mode, path, sftpClient.getHostInfo(), err)
	}
	return nil
}

// CopyRemote copies a file or directory recursively on the remote host using a pooled SFTP client
func CopyRemote(sftpClient *SftpClient, src, dst string) error {
	return copyRemoteRecursive(sftpClient.Client, src, dst)
}
