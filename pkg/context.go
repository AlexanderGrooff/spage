package pkg

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"regexp"

	"github.com/flosch/pongo2"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type Facts map[string]ModuleOutput

func (f *Facts) Merge(other Facts) {
	for key, value := range other {
		(*f)[key] = value
	}
}

func (f *Facts) Add(k string, v ModuleOutput) Facts {
	(*f)[k] = v
	return *f
}

func (f Facts) ToJinja2() pongo2.Context {
	ctx := pongo2.Context{}
	for k, v := range f {
		ctx[k] = v
	}
	return ctx
}

type HostContext struct {
	Host    Host
	Facts   Facts
	History map[string]ModuleOutput
}

func ReadTemplateFile(filename string) (string, error) {
	return ReadLocalFile("templates/" + filename)
}

func ReadLocalFile(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (c HostContext) ReadFile(filename string, username string) (string, error) {
	if c.Host.IsLocal {
		return ReadLocalFile(filename)
	}
	return c.ReadRemoteFile(filename, username)
}

func (c HostContext) ReadRemoteFile(filename string, username string) (string, error) {
	stdout, _, err := RunRemoteCommand(c.Host.Host, fmt.Sprintf("cat \"%s\"", filename), username)
	if err != nil {
		return "", err
	}
	return stdout, nil
}

func (c HostContext) WriteFile(filename, contents, username string) error {
	if c.Host.IsLocal {
		return WriteLocalFile(filename, contents)
	}
	return WriteRemoteFile(c.Host.Host, filename, contents)
}

func WriteLocalFile(filename string, data string) error {
	return os.WriteFile(filename, []byte(data), 0644)
}

func WriteRemoteFile(host, remotePath, data string) error {
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

func (c HostContext) RunCommand(command, username string) (string, string, error) {
	if username == "" {
		user, err := user.Current()
		if err != nil {
			return "", "", fmt.Errorf("failed to get current user: %v", err)
		}
		username = user.Username
	}
	if c.Host.IsLocal {
		return RunLocalCommand(command, username)
	}
	return RunRemoteCommand(c.Host.Host, command, username)
}

func RunLocalCommand(command, username string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	var err error
	cmd := exec.Command("sudo", "-nu", username, "bash", "-c", command)
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
	user, err := user.Current()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current user: %v", err)
	}
	// SSH as the current user
	// TODO: fetch ssh from config/strategy
	config := &ssh.ClientConfig{
		User: user.Username,
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

func TemplateString(s string, additionalVars ...Facts) (string, error) {
	// Create a new pongo2 template
	tmpl, err := pongo2.FromString(s)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %v", err)
	}

	allVars := make(Facts)
	for _, v := range additionalVars {
		allVars.Merge(v)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteWriter(allVars.ToJinja2(), &buf); err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}

	return buf.String(), nil
}

func GetVariableUsageFromString(s string) []string {
	// TODO: this catches everything between the brackets, but we only want variables
	re := regexp.MustCompile(`{{\s*([^{}\s]+)\s*}}`)
	matches := re.FindAllStringSubmatch(s, -1)

	var vars []string
	for _, match := range matches {
		vars = append(vars, match[1])
	}
	return vars
}
