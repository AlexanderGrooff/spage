package modules

import (
	"fmt"
	"reflect"

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
	Dest    string `yaml:"dest"`
	Mode    string `yaml:"mode"`
	pkg.ModuleInput
}

type CopyOutput struct {
	OriginalContents string
	OriginalMode     string
	NewContents      string
	NewMode          string
	pkg.ModuleOutput
}

func (i CopyInput) ToCode() string {
	return fmt.Sprintf("modules.CopyInput{Content: %q, Dest: %q, Mode: %q}",
		i.Content,
		i.Dest,
		i.Mode,
	)
}

func (i CopyInput) GetVariableUsage() []string {
	vars := pkg.GetVariableUsageFromTemplate(i.Content)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Dest)...)
	return vars
}

func (i CopyInput) Validate() error {
	if i.Content == "" {
		return fmt.Errorf("missing Content input")
	}
	if i.Dest == "" {
		return fmt.Errorf("missing Dest input")
	}
	return nil
}

func (o CopyOutput) String() string {
	return fmt.Sprintf("  original contents: %q, mode: %q\n  new contents: %q, mode: %q",
		o.OriginalContents, o.OriginalMode,
		o.NewContents, o.NewMode)
}

func (o CopyOutput) Changed() bool {
	return o.OriginalContents != o.NewContents || o.OriginalMode != o.NewMode
}

func (m CopyModule) Execute(params pkg.ModuleInput, c *pkg.HostContext, runAs string) (pkg.ModuleOutput, error) {
	p := params.(CopyInput)

	// Get original state
	originalContents, _ := c.ReadFile(p.Dest, runAs)
	originalMode := ""
	// Get mode using ls command if file exists
	if originalContents != "" {
		stdout, _, err := c.RunCommand(fmt.Sprintf("ls -l %s | cut -d ' ' -f 1", p.Dest), runAs)
		if err == nil {
			originalMode = stdout[1:4] // Extract numeric mode from ls output
		}
	}

	// Write new content
	if err := c.WriteFile(p.Dest, p.Content, runAs); err != nil {
		return nil, fmt.Errorf("failed to write to file %s: %v", p.Dest, err)
	}

	// Apply mode if specified
	newMode := originalMode
	if p.Mode != "" {
		if _, _, err := c.RunCommand(fmt.Sprintf("chmod %s %s", p.Mode, p.Dest), runAs); err != nil {
			return nil, fmt.Errorf("failed to chmod %s: %v", p.Dest, err)
		}
		newMode = p.Mode
	}

	return CopyOutput{
		OriginalContents: originalContents,
		OriginalMode:     originalMode,
		NewContents:      p.Content,
		NewMode:          newMode,
	}, nil
}

func (m CopyModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	p := params.(CopyInput)
	if previous == nil {
		pkg.DebugOutput("Not reverting because previous result was nil")
		return CopyOutput{}, nil
	}

	prev := previous.(CopyOutput)
	if !prev.Changed() {
		return CopyOutput{}, nil
	}

	// Revert content
	if err := c.WriteFile(p.Dest, prev.OriginalContents, runAs); err != nil {
		return nil, fmt.Errorf("failed to revert contents of %s: %v", p.Dest, err)
	}

	// Revert mode
	if prev.OriginalMode != "" {
		if _, _, err := c.RunCommand(fmt.Sprintf("chmod %s %s", prev.OriginalMode, p.Dest), runAs); err != nil {
			return nil, fmt.Errorf("failed to chmod %s: %v", p.Dest, err)
		}
	}

	return CopyOutput{
		OriginalContents: prev.NewContents,
		OriginalMode:     prev.NewMode,
		NewContents:      prev.OriginalContents,
		NewMode:          prev.OriginalMode,
	}, nil
}

func init() {
	pkg.RegisterModule("copy", CopyModule{})
}
