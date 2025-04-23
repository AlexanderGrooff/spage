package modules

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

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

// AsFacts implements the pkg.FactProvider interface.
// It returns a map representation suitable for registration,
// using lowercase keys for Ansible compatibility.
func (o ShellOutput) AsFacts() map[string]interface{} {
	return map[string]interface{}{
		"stdout":  o.Stdout,
		"stderr":  o.Stderr,
		"command": o.Command,
		"changed": o.Changed(),
	}
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

	// Escape backslashes and double quotes for embedding in a shell double-quoted string.
	escaper := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	escapedCmd := escaper.Replace(templatedCmd)

	// Format the command for "sh -c" using double quotes.
	// This allows internal single quotes and preserves newlines (as \n interpreted by shell).
	commandForShell := fmt.Sprintf("sh -c \"%s\"", escapedCmd)

	// Pass the double-quoted command string to RunCommand
	stdout, stderr, err := c.RunCommand(commandForShell, runAs)
	output := ShellOutput{
		Stdout:  stdout,
		Stderr:  stderr,
		Command: commandForShell, // Store the command passed to RunCommand
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

// UnmarshalYAML implements the yaml.Unmarshaler interface for ShellInput.
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
}

// ParameterAliases defines aliases for module parameters.
func (m ShellModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
