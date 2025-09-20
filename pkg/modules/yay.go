package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
)

type YayModule struct{}

func (sm YayModule) InputType() reflect.Type {
	return reflect.TypeOf(YayInput{})
}

func (sm YayModule) OutputType() reflect.Type {
	return reflect.TypeOf(YayOutput{})
}

// Doc returns module-level documentation rendered into Markdown.
func (sm YayModule) Doc() string {
	return `Manage packages with the yay AUR helper. This module can install or remove packages from the Arch User Repository (AUR) and official repositories on Arch Linux systems.

## Examples

` + "```yaml" + `
- name: Install AUR package
  yay:
    name:
      - google-chrome
    state: present

- name: Install multiple packages
  yay:
    name:
      - discord
      - visual-studio-code-bin
      - spotify
    state: present

- name: Remove AUR package
  yay:
    name:
      - google-chrome
    state: absent
` + "```" + `

**Note**: This module requires yay to be installed and is designed for Arch Linux systems. Yay can install both official packages and AUR packages.
`
}

// ParameterDocs provides rich documentation for yay module inputs.
func (sm YayModule) ParameterDocs() map[string]pkg.ParameterDoc {
	notRequired := false
	return map[string]pkg.ParameterDoc{
		"name": {
			Description: "List of package names to operate on. Can include both official repository and AUR packages.",
			Required:    &notRequired,
			Default:     "",
		},
		"state": {
			Description: "Desired package state.",
			Required:    &notRequired,
			Default:     "present",
			Choices:     []string{"present", "absent"},
		},
	}
}

type YayInput struct {
	Name  []string `yaml:"name"`
	State string   `yaml:"state"`
}

type YayOutput struct {
	Installed []string
	Removed   []string
	Stdout    string
	Stderr    string
	pkg.ModuleOutput
}

func (i YayInput) ToCode() string {
	quotedStrings := make([]string, len(i.Name))
	for idx, name := range i.Name {
		quotedStrings[idx] = fmt.Sprintf("%q", name)
	}
	return fmt.Sprintf("modules.YayInput{Name: []string{%s}}",
		strings.Join(quotedStrings, ", "),
	)
}

func (i YayInput) GetVariableUsage() []string {
	return pkg.GetVariableUsageFromTemplate(strings.Join(i.Name, " "))
}

func (i YayInput) Validate() error {
	if len(i.Name) == 0 {
		return fmt.Errorf("missing Name input")
	}
	return nil
}

func (i YayInput) HasRevert() bool {
	return true
}

func (i YayInput) ProvidesVariables() []string {
	return nil
}

func (o YayOutput) String() string {
	return fmt.Sprintf("  installed: %v\n  removed: %v\n  stdout: %q\n  stderr: %q\n", o.Installed, o.Removed, o.Stdout, o.Stderr)
}

func (o YayOutput) Changed() bool {
	return len(o.Installed) > 0 || len(o.Removed) > 0
}

func IsPackageInstalled(packageName string, c *pkg.HostContext, runAs string) bool {
	_, _, _, err := c.RunCommand(fmt.Sprintf("yay -Qi %s", packageName), runAs)
	return err == nil
}

func (m YayModule) InstallPackages(packages []string, c *pkg.HostContext, runAs string) (YayOutput, error) {
	missingPackages := []string{}
	for _, packageName := range packages {
		if !IsPackageInstalled(packageName, c, runAs) {
			missingPackages = append(missingPackages, packageName)
		}
	}
	if len(missingPackages) > 0 {
		_, stdout, stderr, err := c.RunCommand(fmt.Sprintf("yay -S --noconfirm %s", strings.Join(missingPackages, " ")), runAs)
		if err != nil {
			return YayOutput{
				Stdout: stdout,
				Stderr: stderr,
			}, err
		} else {
			return YayOutput{
				Installed: missingPackages,
				Stdout:    stdout,
				Stderr:    stderr,
			}, nil
		}
	}

	return YayOutput{}, nil
}

func (m YayModule) RemovePackages(packages []string, c *pkg.HostContext, runAs string) (YayOutput, error) {
	presentPackages := []string{}
	for _, packageName := range packages {
		if IsPackageInstalled(packageName, c, runAs) {
			presentPackages = append(presentPackages, packageName)
		}
	}
	if len(presentPackages) > 0 {
		_, stdout, stderr, err := c.RunCommand(fmt.Sprintf("yay -Rns --noconfirm %s", strings.Join(presentPackages, " ")), runAs)
		if err != nil {
			return YayOutput{
				Stdout: stdout,
				Stderr: stderr,
			}, err
		} else {
			return YayOutput{
				Removed: presentPackages,
				Stdout:  stdout,
				Stderr:  stderr,
			}, nil
		}
	}
	return YayOutput{}, nil
}

func (m YayModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	yayParams, ok := params.(YayInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected YayInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected YayInput, got %T", params)
	}

	if err := yayParams.Validate(); err != nil {
		return nil, err
	}

	packages := yayParams.Name
	state := yayParams.State
	checkMode := false
	checkMode = closure.IsCheckMode()

	if checkMode {
		if state == "absent" {
			common.LogDebug("Would remove packages", map[string]interface{}{
				"host":     closure.HostContext.Host.Name,
				"packages": packages,
			})
			presentPackages := []string{}
			for _, packageName := range packages {
				if IsPackageInstalled(packageName, closure.HostContext, runAs) {
					presentPackages = append(presentPackages, packageName)
				}
			}
			return YayOutput{Removed: presentPackages}, nil
		}
		common.LogDebug("Would install packages", map[string]interface{}{
			"host":     closure.HostContext.Host.Name,
			"packages": packages,
		})
		missingPackages := []string{}
		for _, packageName := range packages {
			if !IsPackageInstalled(packageName, closure.HostContext, runAs) {
				missingPackages = append(missingPackages, packageName)
			}
		}
		return YayOutput{Installed: missingPackages}, nil
	}

	if state == "absent" {
		return m.RemovePackages(packages, closure.HostContext, runAs)
	}
	return m.InstallPackages(packages, closure.HostContext, runAs)
}

func (m YayModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	yayParams, ok := params.(YayInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Revert: params is nil, expected YayInput but got nil")
		}
		return nil, fmt.Errorf("Revert: incorrect parameter type: expected YayInput, got %T", params)
	}

	previousPackages := previous.(YayOutput).Installed
	state := yayParams.State
	if state == "absent" {
		return m.InstallPackages(previousPackages, closure.HostContext, runAs)
	} else {
		return m.RemovePackages(previousPackages, closure.HostContext, runAs)
	}
}

func init() {
	pkg.RegisterModule("yay", YayModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m YayModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
