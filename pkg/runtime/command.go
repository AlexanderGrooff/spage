package runtime

import (
	"bytes"
	"fmt"
	"os/exec"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/google/shlex"
	"golang.org/x/crypto/ssh"
)

func RunLocalCommand(command, username string) (string, string, error) {
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
		return "", "", fmt.Errorf("failed to split command %s: %v", command, err)
	}
	prog := splitCmd[0]
	args := splitCmd[1:]
	absProg, err := exec.LookPath(prog)
	if err != nil {
		return "", "", fmt.Errorf("failed to find %s in $PATH: %v", prog, err)
	}
	cmd = exec.Command(absProg, args...)
	common.DebugOutput("Running command: %s\n", cmd.String())
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("failed to execute command %q: %v", cmd.String(), err)
	}

	return stdout.String(), stderr.String(), nil
}

// RunRemoteCommand executes a command on a remote host using an existing SSH client connection.
func RunRemoteCommand(client *ssh.Client, command, username string) (string, string, error) {
	// Each ClientConn can support multiple interactive sessions,
	// represented by a Session. It's one session per command.
	session, err := client.NewSession()
	if err != nil {
		// Include the host address in the error if possible. client.RemoteAddr()
		return "", "", fmt.Errorf("failed to create ssh session to %s: %w", client.RemoteAddr(), err)
	}
	defer session.Close()

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
	if err := session.Run(cmdToRun); err != nil {
		// Include more context in the error message
		return stdout.String(), stderr.String(), fmt.Errorf("failed to run remote command '%s' (original: '%s') on host %s: %w, stderr: %s", cmdToRun, command, client.RemoteAddr(), err, stderr.String())
	}
	return stdout.String(), stderr.String(), nil
}
