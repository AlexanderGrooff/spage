package modules

import (
	"fmt"
	"os"
	"os/exec"
)

type ShellModule struct{}

func (s ShellModule) Execute(params ModuleInput) (ModuleOutput, error) {
	command := params["execute"].(string)
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	output := ModuleOutput{}
	output["stdout"] = cmd.Stdout
	output["stderr"] = cmd.Stderr

	if err != nil {
		return output, fmt.Errorf("failed to execute command: %v", err)
	}

	return output, nil
}

func (s ShellModule) Revert(params ModuleInput) (ModuleOutput, error) {
	command := params["revert"].(string)
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	output := ModuleOutput{}
	output["stdout"] = cmd.Stdout
	output["stderr"] = cmd.Stderr

	if err != nil {
		return output, fmt.Errorf("failed to revert command: %v", err)
	}
	return output, nil
}

func init() {
	RegisterModule("shell", ShellModule{})
}
