package modules

import (
	"fmt"
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg/common"

	"github.com/AlexanderGrooff/spage/pkg"
)

type TemplateModule struct{}

func (sm TemplateModule) InputType() reflect.Type {
	return reflect.TypeOf(TemplateInput{})
}

func (sm TemplateModule) OutputType() reflect.Type {
	return reflect.TypeOf(TemplateOutput{})
}

type TemplateInput struct {
	Src  string `yaml:"src"`
	Dst  string `yaml:"dest"`
	Mode string `yaml:"mode"`
	pkg.ModuleInput
}

type TemplateOutput struct {
	Contents pkg.RevertableChange[string]
	// TODO: track if file was created
	pkg.ModuleOutput
}

func (i TemplateInput) ToCode() string {
	return fmt.Sprintf("modules.TemplateInput{Src: %q, Dst: %q, Mode: %q}",
		i.Src,
		i.Dst,
		i.Mode,
	)
}

func (i TemplateInput) GetVariableUsage() []string {
	usedVars := []string{}
	// TODO: what if the filename itself is templated? Then we cannot read the file until we have context
	template, err := pkg.ReadTemplateFile(i.Src)
	if err == nil {
		usedVars = append(usedVars, pkg.GetVariableUsageFromTemplate(template)...)
	}
	// Get variables from filenames
	usedVars = append(usedVars, pkg.GetVariableUsageFromTemplate(i.Src)...)
	usedVars = append(usedVars, pkg.GetVariableUsageFromTemplate(i.Dst)...)
	return usedVars
}

func (i TemplateInput) Validate() error {
	if i.Src == "" {
		return fmt.Errorf("missing Src input")
	}
	if i.Dst == "" {
		return fmt.Errorf("missing Dst input")
	}
	return nil
}

func (o TemplateOutput) String() string {
	// TODO: show diff
	return fmt.Sprintf("  original: %q\n  new: %q", o.Contents.Before, o.Contents.After)
}

func (o TemplateOutput) Changed() bool {
	return o.Contents.Changed()
}

func (m TemplateModule) templateContentsToFile(src, dest string, closure *pkg.Closure, runAs string) (string, string, error) {
	// Get contents from src
	contents, err := pkg.ReadTemplateFile(src)
	if err != nil {
		return "", "", fmt.Errorf("failed to read template file %s: %v", src, err)
	}
	templatedContents, err := pkg.TemplateString(contents, closure)
	if err != nil {
		return "", "", err
	}

	originalContents, _ := closure.HostContext.ReadFile(dest, runAs)
	if err := closure.HostContext.WriteFile(dest, templatedContents, runAs); err != nil {
		return "", "", fmt.Errorf("failed to write to file %s: %v", dest, err)
	}

	return originalContents, templatedContents, nil
}

func (m TemplateModule) Execute(params pkg.ModuleInput, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	p := params.(TemplateInput)
	original, new, err := m.templateContentsToFile(p.Src, p.Dst, closure, runAs)
	if err != nil {
		return nil, err
	}

	if p.Mode != "" {
		if err := closure.HostContext.SetFileMode(p.Dst, p.Mode, runAs); err != nil {
			// Attempt to revert the content change if setting mode fails
			_ = closure.HostContext.WriteFile(p.Dst, original, runAs) // Best effort revert
			return nil, fmt.Errorf("failed to set mode %s for file %s: %w", p.Mode, p.Dst, err)
		}
	}

	return TemplateOutput{
		Contents: pkg.RevertableChange[string]{
			Before: original,
			After:  new,
		},
	}, nil
}

func (m TemplateModule) Revert(params pkg.ModuleInput, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	// TODO: delete if previously created?
	p := params.(TemplateInput)
	if previous != nil {
		prev := previous.(TemplateOutput)
		if prev.Changed() {
			if err := closure.HostContext.WriteFile(p.Dst, prev.Contents.Before, runAs); err != nil {
				return TemplateOutput{}, fmt.Errorf("failed to place back original contents in %s", p.Dst)
			}
		}
		return TemplateOutput{
			Contents: pkg.RevertableChange[string]{
				Before: prev.Contents.After,
				After:  prev.Contents.Before,
			},
		}, nil
	}
	common.DebugOutput("Not reverting because previous result was %v", previous)
	return TemplateOutput{}, nil
}

func init() {
	pkg.RegisterModule("template", TemplateModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m TemplateModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
