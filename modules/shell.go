package modules

import (
	"fmt"
	"os"
	"os/exec"
)

type ShellModule struct{}

func (s ShellModule) Execute(params map[string]interface{}) error {
	command := params["execute"].(string)
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to execute command: %v", err)
	}
	return nil
}

func (s ShellModule) Revert(params map[string]interface{}) error {
	command := params["revert"].(string)
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to revert command: %v", err)
	}
	return nil
}

func init() {
	RegisterModule("shell", ShellModule{})
}
