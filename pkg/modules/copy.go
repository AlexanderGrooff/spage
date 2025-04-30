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

// HasRevert indicates that the copy module can be reverted.
func (i CopyInput) HasRevert() bool {
	return true
}

func (i CopyInput) Validate() error {
	if i.Content == "" {
		return fmt.Errorf("missing Content input")
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

func (m CopyModule) Execute(params pkg.ModuleInput, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	p := params.(CopyInput)

	// Get original state
	// Ignore error because dst is allowed to not exist
	var err error
	originalContents, _ := closure.HostContext.ReadFile(p.Dst, runAs)
	originalMode := ""
	newContents := ""
	// Get mode using ls command if file exists
	if originalContents != "" {
		// TODO: get mode with Golang if local
		stdout, _, err := closure.HostContext.RunCommand(fmt.Sprintf("ls -l %s | cut -d ' ' -f 1", p.Dst), runAs)
		if err == nil {
			originalMode = stdout[1:4] // Extract numeric mode from ls output
		}
	}

	if p.Src != "" {
		// TODO: copy as user
		common.DebugOutput("Copying %s to %s", p.Src, p.Dst)
		if err := closure.HostContext.Copy(p.Src, p.Dst); err != nil {
			return nil, fmt.Errorf("failed to copy %s to %s: %v", p.Src, p.Dst, err)
		}
		if newContents, err = closure.HostContext.ReadFile(p.Src, runAs); err != nil {
			return nil, fmt.Errorf("failed to read %s: %v", p.Src, err)
		}
	}

	if p.Content != "" {
		if err := closure.HostContext.WriteFile(p.Dst, p.Content, runAs); err != nil {
			return nil, fmt.Errorf("failed to place contents in %s: %v", p.Dst, err)
		}
		newContents = p.Content
	}

	// Apply mode if specified
	newMode := originalMode
	if p.Mode != "" {
		if err := closure.HostContext.SetFileMode(p.Dst, p.Mode, runAs); err != nil {
			return nil, fmt.Errorf("failed to set mode %s on %s: %w", p.Mode, p.Dst, err)
		}
		newMode = p.Mode
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

func (m CopyModule) Revert(params pkg.ModuleInput, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	p := params.(CopyInput)
	if previous == nil {
		common.DebugOutput("Not reverting because previous result was nil")
		return CopyOutput{}, nil
	}

	prev := previous.(CopyOutput)
	if !prev.Changed() {
		return CopyOutput{}, nil
	}

	// Revert content
	if err := closure.HostContext.WriteFile(p.Dst, prev.Contents.Before, runAs); err != nil {
		return nil, fmt.Errorf("failed to revert contents of %s: %v", p.Dst, err)
	}

	// Revert mode
	if prev.Mode.Before != "" {
		if err := closure.HostContext.SetFileMode(p.Dst, prev.Mode.Before, runAs); err != nil {
			return nil, fmt.Errorf("failed to revert mode on %s to %s: %w", p.Dst, prev.Mode.Before, err)
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
}

// ParameterAliases defines aliases for module parameters.
func (cm CopyModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
