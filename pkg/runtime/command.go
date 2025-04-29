package runtime

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/google/shlex"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
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

func RunRemoteCommand(host, command, username string) (string, string, error) {
	// Get active SSH keys from ssh-agent
	socket := os.Getenv("SSH_AUTH_SOCK")
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return "", "", fmt.Errorf("failed to open SSH_AUTH_SOCK: %v", err)
	}
	agentClient := agent.NewClient(conn)
	currentUser, err := user.Current()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current user: %v", err)
	}
	// SSH as the current user
	// TODO: fetch ssh from config/strategy
	config := &ssh.ClientConfig{
		User: currentUser.Username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeysCallback(agentClient.Signers),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	client, err := ssh.Dial("tcp", net.JoinHostPort(host, "22"), config)
	if err != nil {
		return "", "", fmt.Errorf("failed to dial host %s: %w", host, err)
	}
	defer client.Close()

	// Each ClientConn can support multiple interactive sessions,
	// represented by a Session. It's one session per command.
	session, err := client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("failed to create ssh session to %s: %w", host, err)
	}
	defer session.Close()

	// Once a Session is created, you can execute a single command on
	// the remote side using the Run method.
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	var cmdToRun string
	if username != "" {
		cmdToRun = fmt.Sprintf("sudo -u %s sh -c %q", username, command)
	} else {
		cmdToRun = fmt.Sprintf("sh -c %q", command)
	}
	if err := session.Run(cmdToRun); err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("failed to run '%v' on host %s: %w", command, host, err)
	}
	return stdout.String(), stderr.String(), nil
}
