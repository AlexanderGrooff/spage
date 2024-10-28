package modules

import (
	"fmt"
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg"
)

type ShellModule struct{}

func (sm ShellModule) InputType() reflect.Type {
	return reflect.TypeOf(ShellInput{})
}

func (sm ShellModule) OutputType() reflect.Type {
	return reflect.TypeOf(ShellOutput{})
}

type ShellInput struct {
	Execute string `yaml:"execute"`
	Revert  string `yaml:"revert"`
	pkg.ModuleInput
}

type ShellOutput struct {
	Stdout  string `yaml:"stdout"`
	Stderr  string `yaml:"stderr"`
	Command string `yaml:"command"`
	pkg.ModuleOutput
}

func (i ShellInput) ToCode(indent int) string {
	return fmt.Sprintf("modules.ShellInput{Execute: %q, Revert: %q},",
		i.Execute,
		i.Revert,
	)
}

func (o ShellOutput) String() string {
	return fmt.Sprintf("  cmd: %s\n  stdout: %s\n  stderr: %s\n", o.Command, o.Stdout, o.Stderr)
}

func templateAndExecute(command string, c pkg.Context) (ShellOutput, error) {
	var err error
	templatedCmd, err := c.TemplateString(command)
	if err != nil {
		return ShellOutput{}, err
	}
	stdout, stderr, err := pkg.RunLocalCommand(templatedCmd)
	output := ShellOutput{
		Stdout:  stdout,
		Stderr:  stderr,
		Command: templatedCmd,
	}

	if err != nil {
		return output, fmt.Errorf("failed to execute command: %v", err)
	}

	return output, nil
}

func (s ShellModule) Execute(params interface{}, c pkg.Context) (interface{}, error) {
	shellParams := params.(ShellInput)
	return templateAndExecute(shellParams.Execute, c)
}

func (s ShellModule) Revert(params interface{}, c pkg.Context) (interface{}, error) {
	shellParams := params.(ShellInput)
	return templateAndExecute(shellParams.Revert, c)
}

func init() {
	pkg.RegisterModule("shell", ShellModule{})
}
