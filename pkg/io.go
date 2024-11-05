package pkg

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func WriteLocalFile(filename string, data string) error {
	return os.WriteFile(filename, []byte(data), 0644)
}

func WriteRemoteFile(host, remotePath, data, username string) error {
	tmpFile, err := os.CreateTemp("", "tempfile")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(data)); err != nil {
		return fmt.Errorf("failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %v", err)
	}

	_, stderr, err := RunLocalCommand(fmt.Sprintf("scp %q %s:%s", tmpFile.Name(), host, tmpFile.Name()), "")
	if err != nil {
		return fmt.Errorf("failed to transfer file to remote host: %v, %s", err, stderr)
	}
	_, _, err = RunRemoteCommand(host, fmt.Sprintf("mv %s %s", tmpFile.Name(), remotePath), username)
	if err != nil {
		return fmt.Errorf("failed to move file to final location: %v", err)
	}

	return nil
}

func RunLocalCommand(command, username string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	var err error
	var cmd *exec.Cmd
	if username != "" {
		cmd = exec.Command("sudo", "-nu", username, "bash", "-c", command)
	} else {
		cmd = exec.Command("bash", "-c", command)
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("failed to execute command %q: %v", command, err)
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
	cmdAsUser := fmt.Sprintf("sudo -nu %s bash -c %q", username, command)
	if err := session.Run(cmdAsUser); err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("failed to run '%v' on host %s: %w", command, host, err)
	}
	return stdout.String(), stderr.String(), nil
}
