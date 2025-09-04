package runtime

import (
	"fmt"
	"os"
	"strconv"

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

// parseFileMode is used by both local and SSH mode setting.
func parseFileMode(modeStr string) (os.FileMode, error) {
	// Try parsing as octal first
	if mode, err := strconv.ParseUint(modeStr, 8, 32); err == nil {
		return os.FileMode(mode), nil
	}
	// TODO: Add support for symbolic mode strings if needed (e.g., "u+x")
	return 0, fmt.Errorf("invalid file mode string: %q", modeStr)
}
