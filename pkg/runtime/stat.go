package runtime

import (
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

// Use GNU stat --printf for detailed info. Handle non-GNU stat later if needed.
// Format: %a %u %g %s %W %X %Y %Z %F %N %d %i %h %r %b %B %U %G
// OctalPerms UID GID Size CreationTime AccessTime ModifyTime ChangeTime FileType FileName DeviceNum InodeNum HardLinks
// RawDeviceNum(hex) AllocBlocks BlockSize UserName GroupName
var statGNUFlags = `0%a
%u
%g
%s
%W
%X
%Y
%Z
%F
%N
%d
%i
%h
%r
%b
%B
%U
%G
 -L
`
var statMacOSFlags = `%Lp
%u
%g
%z
%B
%a
%m
%c
%LT
%N
%d
%i
0
%r
%b
%k
%gu
%gu
`

// Removed %s %W %X %F %h %U %G

//if p.Follow {
//	statCmd += " -L" // Follow symlinks
//}

// func StatLocal(path, runAs string) (string, string, error) {
// 	// TODO: use go's local os.Stat
// 	fullCmd := fmt.Sprintf("stat --printf=\"%s\" %s", statGNUFlags, path)
// 	stdout, stderr, err := RunLocalCommand(fullCmd, runAs)
// 	if err != nil {
// 		// Try MacOS-specific stat
// 		fullCmd := fmt.Sprintf("stat -f \"%s\" %s", statMacOSFlags, path)
// 		return RunLocalCommand(fullCmd, runAs)
// 	}
// 	return stdout, stderr, err
// }

//	func StatRemote(path, host, runAs string) (string, string, error) {
//		// TODO: separate into StatMacOS, StatGNU
//		fullCmd := fmt.Sprintf("stat --printf=\"%s\" %s", statGNUFlags, path)
//		stdout, stderr, err := RunLocalCommand(fullCmd, runAs)
//		if err != nil {
//			// Try MacOS-specific stat
//			fullCmd := fmt.Sprintf("stat -f \"%s\" %s", statMacOSFlags, path)
//			return RunLocalCommand(fullCmd, runAs)
//		}
//		return stdout, stderr, err
//	}
func StatLocal(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file/dir not found: %s: %w", path, err)
		}
		return nil, fmt.Errorf("failed to stat local path %s: %w", path, err)
	}
	return info, nil
}

func StatRemote(sshClient *ssh.Client, path string) (os.FileInfo, error) {
	sftpClient, err := getSftpClient(sshClient)
	if err != nil {
		return nil, err
	}
	defer sftpClient.Close()

	info, err := sftpClient.Lstat(path) // Use Lstat to handle symlinks correctly
	if err != nil {
		// Keep os.IsNotExist check
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file/dir not found: %s on host %s", path, sshClient.RemoteAddr())
		}
		return nil, fmt.Errorf("failed to stat remote path %s on %s: %w", path, sshClient.RemoteAddr(), err)
	}
	return info, nil
}
