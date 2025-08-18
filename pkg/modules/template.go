package modules

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

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
}

type TemplateOutput struct {
	Contents       pkg.RevertableChange[string] `yaml:"-"` // Don't serialize diff in YAML
	ShouldShowDiff bool                         `yaml:"-"` // New field to control diff display
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
	// Note: GetVariableUsage doesn't have access to closure/role context, so falls back to default template resolution
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

func (i TemplateInput) HasRevert() bool {
	return true
}

func (i TemplateInput) ProvidesVariables() []string {
	return nil
}

func (o TemplateOutput) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  msg: %s\n", o.msg()))
	// Only show diff if ShouldShowDiff is true AND there's actually a change
	if o.ShouldShowDiff && o.Contents.Changed() {
		sb.WriteString("  diff: |\n")
		// Indent each line of the diff for nice output
		diff, err := o.Contents.DiffOutput()
		if err != nil {
			common.LogWarn("failed to generate diff", map[string]interface{}{"error": err})
		}
		for _, line := range strings.Split(diff, "\n") {
			sb.WriteString(fmt.Sprintf("    %s\n", line))
		}
	}
	sb.WriteString(fmt.Sprintf("  new content: %q\n", o.Contents.After))
	return sb.String()
}

// msg returns an appropriate message based on whether content changed
func (o TemplateOutput) msg() string {
	if o.Contents.Changed() {
		return "template file updated"
	}
	return "template file already up to date"
}

func (o TemplateOutput) Changed() bool {
	return o.Contents.Changed()
}

// readRoleAwareTemplateFile reads a template file, checking role-specific paths first if the task is from a role
func (m TemplateModule) readRoleAwareTemplateFile(filename string, closure *pkg.Closure) (string, error) {
	// If this is a role task, check role-specific templates directory first
	if rolePath, ok := closure.GetFact("_spage_role_path"); ok && rolePath != "" {
		if rolePathStr, ok := rolePath.(string); ok {
			roleTemplatePath := filepath.Join(rolePathStr, "templates", filename)
			if content, err := pkg.ReadLocalFile(roleTemplatePath); err == nil {
				return content, nil
			}
		}
	}
	
	// Fallback to default template resolution
	return pkg.ReadTemplateFile(filename)
}

func (m TemplateModule) templateContentsToFile(src, dest string, closure *pkg.Closure, runAs string) (string, string, error) {
	// Get contents from src, checking role-specific paths if available
	contents, err := m.readRoleAwareTemplateFile(src, closure)
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

func (m TemplateModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	templateParams, ok := params.(TemplateInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected TemplateInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected TemplateInput, got %T", params)
	}

	if err := templateParams.Validate(); err != nil {
		return nil, err
	}

	original, new, err := m.templateContentsToFile(templateParams.Src, templateParams.Dst, closure, runAs)
	if err != nil {
		return nil, err
	}

	if templateParams.Mode != "" {
		if err := closure.HostContext.SetFileMode(templateParams.Dst, templateParams.Mode, runAs); err != nil {
			// Attempt to revert the content change if setting mode fails
			_ = closure.HostContext.WriteFile(templateParams.Dst, original, runAs) // Best effort revert
			return nil, fmt.Errorf("failed to set mode %s for file %s: %w", templateParams.Mode, templateParams.Dst, err)
		}
	}

	return TemplateOutput{
		Contents: pkg.RevertableChange[string]{
			Before: original,
			After:  new,
		},
		ShouldShowDiff: pkg.ShouldShowDiff(closure),
	}, nil
}

func (m TemplateModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	templateParams, ok := params.(TemplateInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Revert: params is nil, expected TemplateInput but got nil")
		}
		return nil, fmt.Errorf("Revert: incorrect parameter type: expected TemplateInput, got %T", params)
	}

	// TODO: delete if previously created?
	if previous != nil {
		prev := previous.(TemplateOutput)
		if prev.Changed() {
			if err := closure.HostContext.WriteFile(templateParams.Dst, prev.Contents.Before, runAs); err != nil {
				return TemplateOutput{}, fmt.Errorf("failed to place back original contents in %s", templateParams.Dst)
			}
		}
		return TemplateOutput{
			Contents: pkg.RevertableChange[string]{
				Before: prev.Contents.After,
				After:  prev.Contents.Before,
			},
			ShouldShowDiff: pkg.ShouldShowDiff(closure),
		}, nil
	}
	return TemplateOutput{
		ShouldShowDiff: pkg.ShouldShowDiff(closure),
	}, nil
}

func init() {
	pkg.RegisterModule("template", TemplateModule{})
	pkg.RegisterModule("ansible.builtin.template", TemplateModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m TemplateModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
