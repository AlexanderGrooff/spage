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

func (i ShellInput) ToCode() string {
	return fmt.Sprintf("modules.ShellInput{Execute: %q, Revert: %q}",
		i.Execute,
		i.Revert,
	)
}

func (o ShellOutput) String() string {
	return fmt.Sprintf("  cmd: %q\n  stdout: %q\n  stderr: %q\n", o.Command, o.Stdout, o.Stderr)
}

func (o ShellOutput) Changed() bool {
	return true
}

func templateAndExecute(command string, c pkg.HostContext, prev ShellOutput) (ShellOutput, error) {
	var err error
	templatedCmd, err := pkg.TemplateString(command, c.Facts.Add("Previous", prev))
	if err != nil {
		return ShellOutput{}, err
	}
	stdout, stderr, err := pkg.RunLocalCommand(templatedCmd)
	output := ShellOutput{
		Stdout:  stdout,
		Stderr:  stderr,
		Command: templatedCmd,
	}

	return output, err
}

func (s ShellModule) Execute(params interface{}, c pkg.HostContext) (interface{}, error) {
	shellParams := params.(ShellInput)
	return templateAndExecute(shellParams.Execute, c, ShellOutput{})
}

func (s ShellModule) Revert(params interface{}, c pkg.HostContext, previous interface{}) (interface{}, error) {
	shellParams := params.(ShellInput)
	var prev ShellOutput
	if previous != nil {
		prev = previous.(ShellOutput)
	} else {
		prev = ShellOutput{}
	}
	// TODO: Why does this say previous cmd is "" if it errored?
	return templateAndExecute(shellParams.Revert, c, prev)
}

func init() {
	pkg.RegisterModule("shell", ShellModule{})
}
