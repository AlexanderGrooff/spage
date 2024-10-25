package pkg

import (
	"fmt"
	"os"
	"os/exec"
)

type ShellModule struct{}

func templateAndExecute(command string, c Context) (ModuleOutput, error) {
	cmd := exec.Command("bash", "-c", c.TemplateString(command))
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

func (s ShellModule) Execute(params ModuleInput, c Context) (ModuleOutput, error) {
	command := params["execute"].(string)
	return templateAndExecute(command, c)
}

func (s ShellModule) Revert(params ModuleInput, c Context) (ModuleOutput, error) {
	command := params["revert"].(string)
	return templateAndExecute(command, c)
}

func init() {
	RegisterModule("shell", ShellModule{})
}
