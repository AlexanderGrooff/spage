package modules

import (
	"fmt"
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg/common"

	"github.com/AlexanderGrooff/spage/pkg"
)

type CopyModule struct{}

func (cm CopyModule) InputType() reflect.Type {
	return reflect.TypeOf(CopyInput{})
}

func (cm CopyModule) OutputType() reflect.Type {
	return reflect.TypeOf(CopyOutput{})
}

type CopyInput struct {
	Content string `yaml:"content"`
	Src     string `yaml:"src"`
	Dst     string `yaml:"dest"`
	Mode    string `yaml:"mode"`
}

type CopyOutput struct {
	Contents pkg.RevertableChange[string]
	Mode     pkg.RevertableChange[string]
	pkg.ModuleOutput
}

func (i CopyInput) ToCode() string {
	return fmt.Sprintf("modules.CopyInput{Content: %q, Src: %q, Dst: %q, Mode: %q}",
		i.Content,
		i.Src,
		i.Dst,
		i.Mode,
	)
}

func (i CopyInput) GetVariableUsage() []string {
	vars := pkg.GetVariableUsageFromTemplate(i.Content)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Src)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Dst)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Mode)...)
	return vars
}

func (i CopyInput) ProvidesVariables() []string {
	return nil
}

// HasRevert indicates that the copy module can be reverted.
func (i CopyInput) HasRevert() bool {
	return true
}

func (i CopyInput) Validate() error {
	if i.Content == "" && i.Src == "" {
		return fmt.Errorf("missing Content or Src input")
	}
	if i.Dst == "" {
		return fmt.Errorf("missing Dst input")
	}
	return nil
}

func (o CopyOutput) String() string {
	return fmt.Sprintf("  original contents: %q, mode: %q\n  new contents: %q, mode: %q",
		o.Contents.Before, o.Mode.Before,
		o.Contents.After, o.Mode.After)
}

func (o CopyOutput) Changed() bool {
	return o.Contents.Changed() || o.Mode.Changed()
}

func (m CopyModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	copyParams, ok := params.(CopyInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected CopyInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected CopyInput, got %T", params)
	}

	if err := copyParams.Validate(); err != nil {
		return nil, err
	}

	// Get original state
	// Ignore error because dst is allowed to not exist
	var err error
	originalContents, _ := closure.HostContext.ReadFile(copyParams.Dst, runAs)
	originalMode := ""
	newContents := ""
	// Get mode using ls command if file exists
	if originalContents != "" {
		// TODO: get mode with Golang if local
		_, stdout, _, err := closure.HostContext.RunCommand(fmt.Sprintf("ls -l %s | cut -d ' ' -f 1", copyParams.Dst), runAs)
		if err == nil {
			originalMode = stdout[1:4] // Extract numeric mode from ls output
		}
	}

	if copyParams.Src != "" {
		// TODO: copy as user
		common.DebugOutput("Copying %s to %s", copyParams.Src, copyParams.Dst)
		if err := closure.HostContext.Copy(copyParams.Src, copyParams.Dst); err != nil {
			return nil, fmt.Errorf("failed to copy %s to %s: %v", copyParams.Src, copyParams.Dst, err)
		}
		if newContents, err = closure.HostContext.ReadFile(copyParams.Src, runAs); err != nil {
			return nil, fmt.Errorf("failed to read %s: %v", copyParams.Src, err)
		}
	}

	if copyParams.Content != "" {
		if err := closure.HostContext.WriteFile(copyParams.Dst, copyParams.Content, runAs); err != nil {
			return nil, fmt.Errorf("failed to place contents in %s: %v", copyParams.Dst, err)
		}
		newContents = copyParams.Content
	}

	// Apply mode if specified
	newMode := originalMode
	if copyParams.Mode != "" {
		if err := closure.HostContext.SetFileMode(copyParams.Dst, copyParams.Mode, runAs); err != nil {
			return nil, fmt.Errorf("failed to set mode %s on %s: %w", copyParams.Mode, copyParams.Dst, err)
		}
		newMode = copyParams.Mode
	}

	return CopyOutput{
		Contents: pkg.RevertableChange[string]{
			Before: originalContents,
			After:  newContents,
		},
		Mode: pkg.RevertableChange[string]{
			Before: originalMode,
			After:  newMode,
		},
	}, nil
}

func (m CopyModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	copyParams, ok := params.(CopyInput)
	if !ok {
		if params == nil {
			// If params are nil, and revert is defined by Dest, we might not be able to proceed.
			// However, CopyInput.HasRevert() relies on Dest being set.
			// It's safer to error out if we expect CopyInput but get nil.
			return nil, fmt.Errorf("Revert: params is nil, expected CopyInput but got nil")
		}
		return nil, fmt.Errorf("Revert: incorrect parameter type: expected CopyInput, got %T", params)
	}

	if previous == nil {
		common.DebugOutput("Not reverting because previous result was nil")
		return CopyOutput{}, nil
	}

	prev := previous.(CopyOutput)
	if !prev.Changed() {
		return CopyOutput{}, nil
	}

	// Revert content
	if err := closure.HostContext.WriteFile(copyParams.Dst, prev.Contents.Before, runAs); err != nil {
		return nil, fmt.Errorf("failed to revert contents of %s: %v", copyParams.Dst, err)
	}

	// Revert mode
	if prev.Mode.Before != "" {
		if err := closure.HostContext.SetFileMode(copyParams.Dst, prev.Mode.Before, runAs); err != nil {
			return nil, fmt.Errorf("failed to revert mode on %s to %s: %w", copyParams.Dst, prev.Mode.Before, err)
		}
	}

	// Flip the before and after values
	return CopyOutput{
		Contents: pkg.RevertableChange[string]{
			Before: prev.Contents.After,
			After:  prev.Contents.Before,
		},
		Mode: pkg.RevertableChange[string]{
			Before: prev.Mode.After,
			After:  prev.Mode.Before,
		},
	}, nil
}

func init() {
	pkg.RegisterModule("copy", CopyModule{})
	pkg.RegisterModule("ansible.builtin.copy", CopyModule{})
}

// ParameterAliases defines aliases for module parameters.
func (cm CopyModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
