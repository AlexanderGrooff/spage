package pkg

import (
	"bytes"
	"fmt"
	"os/exec"
	"reflect"
)

type ShellModule struct{}

type ShellInput struct {
	Execute string `yaml:"execute"`
	Revert  string `yaml:"revert"`
	ModuleInput
}
type ShellOutput struct {
	ExitCode int    `yaml:"exit_code"`
	Stdout   string `yaml:"stdout"`
	Stderr   string `yaml:"stderr"`
	Command  string `yaml:"command"`
	ModuleOutput
}

func (sm ShellModule) InputType() reflect.Type {
	return reflect.TypeOf(ShellInput{})
}

func (sm ShellModule) OutputType() reflect.Type {
	return reflect.TypeOf(ShellOutput{})
}

func (i ShellInput) ToCode(indent int) string {
	return fmt.Sprintf("pkg.ShellInput{Execute: %q, Revert: %q},",
		// Indent(indent),
		i.Execute,
		i.Revert,
	)
}

func (o ShellOutput) String() string {
	return fmt.Sprintf("  cmd: %s\n  exitcode: %d\n  stdout: %s\n  stderr: %s\n", o.Command, o.ExitCode, o.Stdout, o.Stderr)
}

func templateAndExecute(command string, c Context) (ShellOutput, error) {
	var stdout, stderr bytes.Buffer
	templatedCmd := c.TemplateString(command)
	cmd := exec.Command("bash", "-c", templatedCmd)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := ShellOutput{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: cmd.ProcessState.ExitCode(),
		Command:  cmd.String(),
	}

	if err != nil {
		return output, fmt.Errorf("failed to execute command: %v", err)
	}

	return output, nil
}

func (s ShellModule) Execute(params interface{}, c Context) (interface{}, error) {
	shellParams := params.(ShellInput)
	return templateAndExecute(shellParams.Execute, c)
}

func (s ShellModule) Revert(params interface{}, c Context) (interface{}, error) {
	shellParams := params.(ShellInput)
	return templateAndExecute(shellParams.Revert, c)
}

func init() {
	RegisterModule("shell", ShellModule{})
}
