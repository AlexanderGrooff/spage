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
	Stdout string `yaml:"stdout"`
	Stderr string `yaml:"stderr"`
	ModuleOutput
}

func (sm ShellModule) InputType() reflect.Type {
	return reflect.TypeOf(ShellInput{})
}

func (sm ShellModule) OutputType() reflect.Type {
	return reflect.TypeOf(ShellOutput{})
}

func (i ShellInput) ToCode(indent int) string {
	return fmt.Sprintf("%spkg.ShellInput{Execute: %q, Revert: %q},\n",
		Indent(indent),
		i.Execute,
		i.Revert,
	)
}

func templateAndExecute(command string, c Context) (ShellOutput, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("bash", "-c", c.TemplateString(command))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := ShellOutput{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	fmt.Printf("Output: %v\n", output)

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
