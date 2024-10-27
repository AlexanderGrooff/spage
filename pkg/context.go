package pkg

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"text/template"
)

type Context map[string]interface{}

func (c Context) TemplateString(s string) (string, error) {
	tmpl, err := template.New("tmpl").Parse(s)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %v", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, c)
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
