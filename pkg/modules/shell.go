package modules

import (
	"fmt"
	"reflect"
	"sync"

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

	// Create a temporary map for the 'Previous' fact
	prevFactMap := new(sync.Map)
	if prev != (ShellOutput{}) { // Only add if 'prev' is not the zero value
		pkg.AddFact(prevFactMap, "Previous", prev)
	}

	// Pass both the main Facts map and the temporary map to TemplateString
	templatedCmd, err := pkg.TemplateString(command, c.Facts, prevFactMap)
	if err != nil {
		return ShellOutput{}, fmt.Errorf("failed to template shell command: %w", err) // Added error wrapping
	}

	// Quote the command properly for sh -c
	// Using fmt.Sprintf with %q handles potential quotes within the command itself
	templatedShell := fmt.Sprintf("sh -c %q", templatedCmd)

	stdout, stderr, err := c.RunCommand(templatedShell, runAs)
	output := ShellOutput{
		Stdout:  stdout,
		Stderr:  stderr,
		Command: templatedShell, // Store the final command executed
	}

	// Don't wrap the error from RunCommand if it's nil
	if err != nil {
		return output, fmt.Errorf("shell command execution failed: %w", err) // Added error wrapping
	}
	return output, nil
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
