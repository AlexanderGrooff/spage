package runtime

import (
	"os"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"golang.org/x/crypto/ssh"
)

// StatLocal retrieves local file information. If follow is true, it follows symlinks (os.Stat).
// If follow is false, it stats the link itself (os.Lstat).
func StatLocal(path string, follow bool) (os.FileInfo, error) {
	if follow {
		return os.Stat(path)
	} else {
		return os.Lstat(path)
	}
}

// StatRemote retrieves remote file information using SFTP Lstat (does not follow symlinks).
// TODO: Implement follow=true for remote if needed (e.g., sftpClient.Stat or ReadLink+Stat)
func StatRemote(sshClient *ssh.Client, path string) (os.FileInfo, error) {
	sftpClient, err := getSftpClient(sshClient)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := sftpClient.Close(); err != nil {
			common.LogWarn("Failed to close SFTP client", map[string]interface{}{
				"host":  sshClient.RemoteAddr().String(),
				"error": err.Error(),
			})
		}
	}()

	return sftpClient.Lstat(path) // Use Lstat to handle symlinks correctly
}
