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

func (i ShellInput) GetVariableUsage() []string {
	return append(pkg.GetVariableUsageFromTemplate(i.Execute), pkg.GetVariableUsageFromTemplate(i.Revert)...)
}

func (i ShellInput) Validate() error {
	if i.Execute == "" && i.Revert == "" {
		return fmt.Errorf("missing both Execute and Revert params. At least one should be given")
	}
	return nil
}

func (o ShellOutput) String() string {
	return fmt.Sprintf("  cmd: %q\n  stdout: %q\n  stderr: %q\n", o.Command, o.Stdout, o.Stderr)
}

func (o ShellOutput) Changed() bool {
	return true
}

func (m ShellModule) templateAndExecute(command string, c *pkg.HostContext, prev ShellOutput, runAs string) (ShellOutput, error) {
	var err error
	templatedCmd, err := pkg.TemplateString(command, c.Facts.Add("Previous", prev))
	if err != nil {
		return ShellOutput{}, err
	}
	templatedShell := fmt.Sprintf("sh -c %q", templatedCmd)
	stdout, stderr, err := c.RunCommand(templatedShell, runAs)
	output := ShellOutput{
		Stdout:  stdout,
		Stderr:  stderr,
		Command: templatedShell,
	}

	return output, err
}

func (m ShellModule) Execute(params pkg.ModuleInput, c *pkg.HostContext, runAs string) (pkg.ModuleOutput, error) {
	shellParams := params.(ShellInput)
	return m.templateAndExecute(shellParams.Execute, c, ShellOutput{}, runAs)
}

func (m ShellModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	shellParams := params.(ShellInput)
	var prev ShellOutput
	if previous != nil {
		prev = previous.(ShellOutput)
	} else {
		prev = ShellOutput{}
	}
	return m.templateAndExecute(shellParams.Revert, c, prev, runAs)
}

func init() {
	pkg.RegisterModule("shell", ShellModule{})
}
