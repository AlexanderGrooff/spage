package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"gopkg.in/yaml.v3"
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
}

type ShellOutput struct {
	Stdout      string   `yaml:"stdout"`
	Stderr      string   `yaml:"stderr"`
	Command     string   `yaml:"command"`
	StdoutLines []string `yaml:"stdout_lines"`
	StderrLines []string `yaml:"stderr_lines"`
	pkg.ModuleOutput
	Rc int `json:"rc"`
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

// AsFacts implements the pkg.FactProvider interface.
// It returns a map representation suitable for registration,
// using lowercase keys for Ansible compatibility.
func (o ShellOutput) AsFacts() map[string]interface{} {
	return map[string]interface{}{
		"stdout":       o.Stdout,
		"stderr":       o.Stderr,
		"rc":           o.Rc,
		"stdout_lines": o.StdoutLines,
		"stderr_lines": o.StderrLines,
	}
}

func (m ShellModule) templateAndExecute(command string, closure *pkg.Closure, prev *ShellOutput, runAs string) (ShellOutput, error) {
	var err error

	if prev != nil {
		closure.ExtraFacts["Previous"] = prev
	}

	// Pass both the main Facts map and the temporary map to TemplateString
	templatedCmd, err := pkg.TemplateString(command, closure)
	if err != nil {
		return ShellOutput{}, fmt.Errorf("failed to template shell command: %w", err) // Added error wrapping
	}

	// Execute the command through a shell using the runtime layer's shell support
	rc, stdout, stderr, err := closure.HostContext.RunCommandWithShell(templatedCmd, runAs, true)
	output := ShellOutput{
		Stdout:      stdout,
		Stderr:      stderr,
		Command:     templatedCmd, // Store the original templated command
		Rc:          rc,
		StdoutLines: strings.Split(stdout, "\n"),
		StderrLines: strings.Split(stderr, "\n"),
	}

	// Don't wrap the error from RunCommand if it's nil
	if err != nil {
		return output, fmt.Errorf("shell command execution failed: %w", err) // Added error wrapping
	}
	return output, nil
}

func (m ShellModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	if closure.IsCheckMode() {
		return ShellOutput{}, nil
	}

	// Type assert params to ShellInput
	shellParams, ok := params.(ShellInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected ShellInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected ShellInput, got %T", params)
	}
	return m.templateAndExecute(shellParams.Execute, closure, nil, runAs)
}

func (m ShellModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	// Type assert params to ShellInput
	shellParams, ok := params.(ShellInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Revert: params is nil, expected ShellInput but got nil")
		}
		return nil, fmt.Errorf("Revert: incorrect parameter type: expected ShellInput, got %T", params)
	}
	var prev ShellOutput
	if previous != nil {
		prev = previous.(ShellOutput)
	} else {
		prev = ShellOutput{}
	}
	return m.templateAndExecute(shellParams.Revert, closure, &prev, runAs)
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for ShellInput.
// This remains important as Task.UnmarshalYAML will delegate decoding of the params node
// to this method if the input type is ShellInput.
// It allows the shell module value to be either a string (shorthand for execute)
// or a map with 'execute' and optionally 'revert' keys.
func (i *ShellInput) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
		// Handle shorthand: shell: command_string
		i.Execute = node.Value
		i.Revert = "" // Default revert for shorthand
		return nil
	}

	if node.Kind == yaml.MappingNode {
		// Handle standard map format: shell: { execute: ..., revert: ... }
		// We need a temporary type to avoid infinite recursion with UnmarshalYAML
		type ShellInputMap struct {
			Execute string `yaml:"execute"`
			Revert  string `yaml:"revert"`
		}
		var tmp ShellInputMap
		if err := node.Decode(&tmp); err != nil {
			return fmt.Errorf("failed to decode shell input map: %w", err)
		}
		i.Execute = tmp.Execute
		i.Revert = tmp.Revert
		return nil
	}

	return fmt.Errorf("invalid type for shell module input: expected string or map, got %v", node.Tag)
}

func init() {
	pkg.RegisterModule("shell", ShellModule{})
	pkg.RegisterModule("ansible.builtin.shell", ShellModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m ShellModule) ParameterAliases() map[string]string {
	return map[string]string{
		"cmd": "execute",
	}
}

// HasRevert checks if a revert command is defined.
func (i ShellInput) HasRevert() bool {
	return i.Revert != ""
}

// ProvidesVariables returns nil as shell input doesn't inherently define variables.
func (i ShellInput) ProvidesVariables() []string {
	return nil
}
