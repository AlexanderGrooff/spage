package runtime

import (
	"fmt"
)

// Use GNU stat --printf for detailed info. Handle non-GNU stat later if needed.
// Format: %a %u %g %s %W %X %Y %Z %F %N %d %i %h %r %b %B %U %G
// OctalPerms UID GID Size CreationTime AccessTime ModifyTime ChangeTime FileType FileName DeviceNum InodeNum HardLinks
// RawDeviceNum(hex) AllocBlocks BlockSize UserName GroupName
var statGNUFlags = `%a
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

func StatLocal(path, runAs string) (string, string, error) {
	// TODO: use go's local os.Stat
	fullCmd := fmt.Sprintf("stat --printf=\"%s\" %s", statGNUFlags, path)
	stdout, stderr, err := RunLocalCommand(fullCmd, runAs)
	if err != nil {
		// Try MacOS-specific stat
		fullCmd := fmt.Sprintf("stat -f \"%s\" %s", statMacOSFlags, path)
		return RunLocalCommand(fullCmd, runAs)
	}
	return stdout, stderr, err
}

func StatRemote(path, host, runAs string) (string, string, error) {
	// TODO: separate into StatMacOS, StatGNU
	fullCmd := fmt.Sprintf("stat --printf=\"%s\" %s", statGNUFlags, path)
	stdout, stderr, err := RunLocalCommand(fullCmd, runAs)
	if err != nil {
		// Try MacOS-specific stat
		fullCmd := fmt.Sprintf("stat -f \"%s\" %s", statMacOSFlags, path)
		return RunLocalCommand(fullCmd, runAs)
	}
	return stdout, stderr, err
}
