package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
)

type PacmanModule struct{}

func (sm PacmanModule) InputType() reflect.Type {
	return reflect.TypeOf(PacmanInput{})
}

func (sm PacmanModule) OutputType() reflect.Type {
	return reflect.TypeOf(PacmanOutput{})
}

type PacmanInput struct {
	Name  []string `yaml:"name"`
	State string   `yaml:"state"`
	pkg.ModuleInput
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

func (o PacmanOutput) String() string {
	return fmt.Sprintf("  installed: %v\n  removed: %v\n  stdout: %q\n  stderr: %q\n", o.Installed, o.Removed, o.Stdout, o.Stderr)
}

func (o PacmanOutput) Changed() bool {
	return len(o.Installed) > 0 || len(o.Removed) > 0
}

func (m PacmanModule) IsPackageInstalled(packageName string, c *pkg.HostContext, runAs string) bool {
	_, _, err := c.RunCommand(fmt.Sprintf("pacman -Qi %s", packageName), runAs)
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
		stdout, stderr, err := c.RunCommand(fmt.Sprintf("pacman -S --noconfirm %s", strings.Join(missingPackages, " ")), runAs)
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
		stdout, stderr, err := c.RunCommand(fmt.Sprintf("pacman -Rns --noconfirm %s", strings.Join(presentPackages, " ")), runAs)
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

func (m PacmanModule) Execute(params pkg.ModuleInput, c *pkg.HostContext, runAs string) (pkg.ModuleOutput, error) {
	packages := params.(PacmanInput).Name
	state := params.(PacmanInput).State
	if state == "absent" {
		return m.RemovePackages(packages, c, runAs)
	} else {
		return m.InstallPackages(packages, c, runAs)
	}
}

func (m PacmanModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	previousPackages := previous.(PacmanOutput).Installed
	state := params.(PacmanInput).State
	if state == "absent" {
		return m.InstallPackages(previousPackages, c, runAs)
	} else {
		return m.RemovePackages(previousPackages, c, runAs)
	}
}

func init() {
	pkg.RegisterModule("pacman", PacmanModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m PacmanModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
