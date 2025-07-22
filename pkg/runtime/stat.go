package runtime

import (
	"os"
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

// StatRemote retrieves remote file information using a pooled SFTP client
func StatRemote(sftpClient *SftpClient, path string) (os.FileInfo, error) {
	return sftpClient.Lstat(path) // Use Lstat to handle symlinks correctly
}
