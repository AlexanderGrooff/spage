package pkg

import (
	"bytes"
	"fmt"
	"golang.org/x/crypto/ssh"
	"net"
	"os"
	"os/exec"
	"text/template"
)

type Facts map[string]interface{}
type Context struct {
	Host  Host
	Facts Facts
}

func (c Context) TemplateString(s string) (string, error) {
	tmpl, err := template.New("tmpl").Parse(s)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %v", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, c.Facts)
	if err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}

	return buf.String(), nil
}

func (c Context) ReadTemplateFile(filename string) (string, error) {
	return c.ReadLocalFile("templates/" + filename)
}

func (c Context) ReadLocalFile(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c Context) WriteLocalFile(filename string, data string) error {
	return os.WriteFile(filename, []byte(data), 0644)
}

func (c Context) WriteRemoteFile(host, remotePath, data string) error {
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

	cmd := exec.Command("scp", tmpFile.Name(), fmt.Sprintf("%s:%s", host, remotePath))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to execute scp command: %v, %s", err, stderr.String())
	}

	return nil
}

func RunLocalCommand(command string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	var err error
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("failed to execute command: %v", err)
	}

	return stdout.String(), stderr.String(), nil
}

func RunRemoteCommand(host, command string) (string, string, error) {
	key, err := ssh.ParsePrivateKey([]byte("BLABLABLA"))
	config := &ssh.ClientConfig{
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(key),
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
	if err := session.Run(command); err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("failed to run '%v' on host %s: %w", command, host, err)
	}
	return stdout.String(), stderr.String(), nil
}
