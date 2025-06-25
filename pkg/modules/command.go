package modules

import (
	"fmt"
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg"
	"gopkg.in/yaml.v3"
)

// CommandModule implements the Ansible 'command' module logic.
// It executes commands directly without going through a shell, meaning
// variables like $HOME and operations like "<", ">", "|", ";", "&" will not work.
type CommandModule struct{}

func (cm CommandModule) InputType() reflect.Type {
	return reflect.TypeOf(CommandInput{})
}

func (cm CommandModule) OutputType() reflect.Type {
	return reflect.TypeOf(CommandOutput{})
}

// CommandInput defines the parameters for the command module.
type CommandInput struct {
	Execute string `yaml:"execute"` // The command to execute. Aliased as 'cmd'.
	Revert  string `yaml:"revert"`  // The command to execute for reverting changes.
}

// CommandOutput defines the output of the command module.
type CommandOutput struct {
	Stdout  string `yaml:"stdout"`
	Stderr  string `yaml:"stderr"`
	Command string `yaml:"command"` // The actual command executed after templating.
	Rc      int    `yaml:"rc"`
	pkg.ModuleOutput
}

// ToCode converts the CommandInput struct into its Go code representation.
func (i CommandInput) ToCode() string {
	return fmt.Sprintf("modules.CommandInput{Execute: %q, Revert: %q}",
		i.Execute,
		i.Revert,
	)
}

func (i CommandInput) ProvidesVariables() []string {
	return nil
}

// GetVariableUsage identifies variables used in the execute and revert commands.
func (i CommandInput) GetVariableUsage() []string {
	return append(pkg.GetVariableUsageFromTemplate(i.Execute), pkg.GetVariableUsageFromTemplate(i.Revert)...)
}

// Validate checks if the input parameters are valid.
func (i CommandInput) Validate() error {
	if i.Execute == "" && i.Revert == "" {
		return fmt.Errorf("missing both Execute and Revert params for command module. At least one should be given")
	}
	return nil
}

// HasRevert checks if a revert command is defined.
func (i CommandInput) HasRevert() bool {
	return i.Revert != ""
}

// String provides a string representation of the CommandOutput.
func (o CommandOutput) String() string {
	return fmt.Sprintf("  cmd: %q\n  stdout: %q\n  stderr: %q\n", o.Command, o.Stdout, o.Stderr)
}

// Changed indicates if the command potentially changed the system state.
// For the command module, we assume it always potentially changes state.
func (o CommandOutput) Changed() bool {
	return true
}

// AsFacts implements the pkg.FactProvider interface.
// It returns a map representation suitable for registration.
func (o CommandOutput) AsFacts() map[string]interface{} {
	return map[string]interface{}{
		"stdout":  o.Stdout,
		"stderr":  o.Stderr,
		"command": o.Command,
		"changed": o.Changed(),
		"rc":      o.Rc,
	}
}

// templateAndExecute templates the command string and executes it directly.
func (m CommandModule) templateAndExecute(command string, closure *pkg.Closure, prev CommandOutput, runAs string) (CommandOutput, error) {
	var err error

	if prev != (CommandOutput{}) {
		closure.ExtraFacts["Previous"] = prev
	}

	// Template the command string using host facts and previous task facts
	templatedCmd, err := pkg.TemplateString(command, closure)
	if err != nil {
		return CommandOutput{}, fmt.Errorf("failed to template command: %w", err)
	}

	// Execute the command directly without shell interpolation.
	// RunCommand needs to handle this case appropriately (e.g., using exec.Command directly).
	// We pass the raw templated command.
	rc, stdout, stderr, err := closure.HostContext.RunCommand(templatedCmd, runAs) // <= NO shell wrapper
	output := CommandOutput{
		Stdout:  stdout,
		Stderr:  stderr,
		Command: templatedCmd, // Store the templated command that was executed
		Rc:      rc,
	}

	if err != nil {
		// Include stdout/stderr in the error message for better debugging
		errMsg := fmt.Sprintf("command execution failed: %s", err.Error())
		if stdout != "" {
			errMsg += fmt.Sprintf("\nstdout: %s", stdout)
		}
		if stderr != "" {
			errMsg += fmt.Sprintf("\nstderr: %s", stderr)
		}
		return output, fmt.Errorf(errMsg)
	}
	return output, nil
}

// Execute runs the main command.
func (cm CommandModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	if checkMode, ok := closure.GetFact("ansible_check_mode"); ok && checkMode.(bool) {
		return CommandOutput{}, nil
	}
	commandParams, ok := params.(CommandInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected CommandInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected CommandInput, got %T", params)
	}
	return cm.templateAndExecute(commandParams.Execute, closure, CommandOutput{}, runAs)
}

// Revert runs the revert command.
func (cm CommandModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	commandParams, ok := params.(CommandInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Revert: params is nil, expected CommandInput but got nil")
		}
		return nil, fmt.Errorf("Revert: incorrect parameter type: expected CommandInput, got %T", params)
	}

	var prev CommandOutput
	if previous != nil {
		prev = previous.(CommandOutput)
	} else {
		prev = CommandOutput{}
	}
	return cm.templateAndExecute(commandParams.Revert, closure, prev, runAs)
}

// UnmarshalYAML allows the command module value to be either a string (shorthand)
// or a map with 'execute' and optionally 'revert'.
func (i *CommandInput) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && (node.Tag == "!!str" || node.Tag == "") { // Allow untagged scalars too
		// Handle shorthand: command: command_string
		i.Execute = node.Value
		i.Revert = "" // Default revert for shorthand is empty
		return nil
	}

	if node.Kind == yaml.MappingNode {
		// Handle standard map format: command: { execute: ..., revert: ... }
		// Use a temporary type to avoid recursion
		type CommandInputMap struct {
			Execute string `yaml:"execute"`
			Revert  string `yaml:"revert"`
			Cmd     string `yaml:"cmd"` // Alias for execute
		}
		var tmp CommandInputMap
		if err := node.Decode(&tmp); err != nil {
			// Provide more context on decode failure
			return fmt.Errorf("failed to decode command input map (line %d): %w", node.Line, err)
		}

		// Handle alias 'cmd' for 'execute'
		if tmp.Execute != "" && tmp.Cmd != "" {
			return fmt.Errorf("cannot specify both 'execute' and 'cmd' for command module (line %d)", node.Line)
		}
		if tmp.Cmd != "" {
			i.Execute = tmp.Cmd
		} else {
			i.Execute = tmp.Execute
		}
		i.Revert = tmp.Revert
		return nil
	}

	return fmt.Errorf("invalid type for command module input (line %d): expected string or map, got %s", node.Line, node.Tag)
}

func init() {
	pkg.RegisterModule("command", CommandModule{})
	pkg.RegisterModule("ansible.builtin.command", CommandModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m CommandModule) ParameterAliases() map[string]string {
	// Define 'cmd' as an alias for 'execute' for compatibility
	return map[string]string{
		"cmd": "execute",
	}
}

func (m CommandModule) SupportsCheckMode() bool {
	return true
}
