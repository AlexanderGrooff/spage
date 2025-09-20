package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
)

type PacmanModule struct{}

func (sm PacmanModule) InputType() reflect.Type {
	return reflect.TypeOf(PacmanInput{})
}

func (sm PacmanModule) OutputType() reflect.Type {
	return reflect.TypeOf(PacmanOutput{})
}

// Doc returns module-level documentation rendered into Markdown.
func (sm PacmanModule) Doc() string {
	return `Manage packages with the pacman package manager. This module can install or remove packages on Arch Linux systems.

## Examples

` + "```yaml" + `
- name: Install a package
  pacman:
    name:
      - nginx
    state: present

- name: Install multiple packages
  pacman:
    name:
      - git
      - vim
      - htop
    state: present

- name: Remove a package
  pacman:
    name:
      - apache
    state: absent
` + "```" + `

**Note**: This module requires pacman to be installed and is designed for Arch Linux and Arch-based distributions.
`
}

// ParameterDocs provides rich documentation for pacman module inputs.
func (sm PacmanModule) ParameterDocs() map[string]pkg.ParameterDoc {
	notRequired := false
	return map[string]pkg.ParameterDoc{
		"name": {
			Description: "List of package names to operate on.",
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

type PacmanInput struct {
	Name  []string `yaml:"name"`
	State string   `yaml:"state"`
}

type PacmanOutput struct {
	Installed []string
	Removed   []string
	Stdout    string
	Stderr    string
	pkg.ModuleOutput
}

func (i PacmanInput) ToCode() string {
	quotedStrings := make([]string, len(i.Name))
	for idx, name := range i.Name {
		quotedStrings[idx] = fmt.Sprintf("%q", name)
	}
	return fmt.Sprintf("modules.PacmanInput{Name: []string{%s}}",
		strings.Join(quotedStrings, ", "),
	)
}

func (i PacmanInput) GetVariableUsage() []string {
	return pkg.GetVariableUsageFromTemplate(strings.Join(i.Name, " "))
}

func (i PacmanInput) Validate() error {
	if len(i.Name) == 0 {
		return fmt.Errorf("missing Name input")
	}
	return nil
}

func (i PacmanInput) ProvidesVariables() []string {
	return nil
}

func (i PacmanInput) HasRevert() bool {
	return true
}

func (o PacmanOutput) String() string {
	return fmt.Sprintf("  installed: %v\n  removed: %v\n  stdout: %q\n  stderr: %q\n", o.Installed, o.Removed, o.Stdout, o.Stderr)
}

func (o PacmanOutput) Changed() bool {
	return len(o.Installed) > 0 || len(o.Removed) > 0
}

func (m PacmanModule) IsPackageInstalled(packageName string, c *pkg.HostContext, runAs string) bool {
	_, _, _, err := c.RunCommand(fmt.Sprintf("pacman -Qi %s", packageName), runAs)
	return err == nil
}

func (m PacmanModule) InstallPackages(packages []string, c *pkg.HostContext, runAs string) (PacmanOutput, error) {
	missingPackages := []string{}
	for _, packageName := range packages {
		if !m.IsPackageInstalled(packageName, c, runAs) {
			missingPackages = append(missingPackages, packageName)
		}
	}
	if len(missingPackages) > 0 {
		_, stdout, stderr, err := c.RunCommand(fmt.Sprintf("pacman -S --noconfirm %s", strings.Join(missingPackages, " ")), runAs)
		if err != nil {
			return PacmanOutput{
				Stdout: stdout,
				Stderr: stderr,
			}, err
		} else {
			return PacmanOutput{
				Installed: missingPackages,
				Stdout:    stdout,
				Stderr:    stderr,
			}, nil
		}
	}

	return PacmanOutput{}, nil
}

func (m PacmanModule) RemovePackages(packages []string, c *pkg.HostContext, runAs string) (PacmanOutput, error) {
	presentPackages := []string{}
	for _, packageName := range packages {
		if m.IsPackageInstalled(packageName, c, runAs) {
			presentPackages = append(presentPackages, packageName)
		}
	}
	if len(presentPackages) > 0 {
		_, stdout, stderr, err := c.RunCommand(fmt.Sprintf("pacman -Rns --noconfirm %s", strings.Join(presentPackages, " ")), runAs)
		if err != nil {
			return PacmanOutput{
				Stdout: stdout,
				Stderr: stderr,
			}, err
		} else {
			return PacmanOutput{
				Removed: presentPackages,
				Stdout:  stdout,
				Stderr:  stderr,
			}, nil
		}
	}
	return PacmanOutput{}, nil
}

func (m PacmanModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	pacmanParams, ok := params.(PacmanInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected PacmanInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected PacmanInput, got %T", params)
	}

	if err := pacmanParams.Validate(); err != nil {
		return nil, err
	}

	packages := pacmanParams.Name
	state := pacmanParams.State
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
				if m.IsPackageInstalled(packageName, closure.HostContext, runAs) {
					presentPackages = append(presentPackages, packageName)
				}
			}
			return PacmanOutput{Removed: presentPackages}, nil
		}
		common.LogDebug("Would install packages", map[string]interface{}{
			"host":     closure.HostContext.Host.Name,
			"packages": packages,
		})
		missingPackages := []string{}
		for _, packageName := range packages {
			if !m.IsPackageInstalled(packageName, closure.HostContext, runAs) {
				missingPackages = append(missingPackages, packageName)
			}
		}
		return PacmanOutput{Installed: missingPackages}, nil
	}

	if state == "absent" {
		return m.RemovePackages(packages, closure.HostContext, runAs)
	}
	return m.InstallPackages(packages, closure.HostContext, runAs)
}

func (m PacmanModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	pacmanParams, ok := params.(PacmanInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Revert: params is nil, expected PacmanInput but got nil")
		}
		return nil, fmt.Errorf("Revert: incorrect parameter type: expected PacmanInput, got %T", params)
	}

	previousPackages := previous.(PacmanOutput).Installed
	state := pacmanParams.State
	if state == "absent" {
		return m.InstallPackages(previousPackages, closure.HostContext, runAs)
	} else {
		return m.RemovePackages(previousPackages, closure.HostContext, runAs)
	}
}

func init() {
	pkg.RegisterModule("pacman", PacmanModule{})
	pkg.RegisterModule("ansible.builtin.pacman", PacmanModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m PacmanModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
