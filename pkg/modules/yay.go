package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
)

type YayModule struct{}

func (sm YayModule) InputType() reflect.Type {
	return reflect.TypeOf(YayInput{})
}

func (sm YayModule) OutputType() reflect.Type {
	return reflect.TypeOf(YayOutput{})
}

type YayInput struct {
	Name  []string `yaml:"name"`
	State string   `yaml:"state"`
	pkg.ModuleInput
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
	return pkg.GetVariableUsageFromString(strings.Join(i.Name, " "))
}

func (i YayInput) Validate() error {
	if len(i.Name) == 0 {
		return fmt.Errorf("missing Name input")
	}
	return nil
}

func (o YayOutput) String() string {
	return fmt.Sprintf("  installed: %v\n  removed: %v\n  stdout: %q\n  stderr: %q\n", o.Installed, o.Removed, o.Stdout, o.Stderr)
}

func (o YayOutput) Changed() bool {
	return len(o.Installed) > 0
}

func IsPackageInstalled(packageName string, c *pkg.HostContext, runAs string) bool {
	_, _, err := c.RunCommand(fmt.Sprintf("yay -Qi %s", packageName), runAs)
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
		stdout, stderr, err := c.RunCommand(fmt.Sprintf("yay -S --noconfirm %s", strings.Join(missingPackages, " ")), runAs)
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
		stdout, stderr, err := c.RunCommand(fmt.Sprintf("yay -Rns --noconfirm %s", strings.Join(presentPackages, " ")), runAs)
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

func (m YayModule) Execute(params pkg.ModuleInput, c *pkg.HostContext, runAs string) (pkg.ModuleOutput, error) {
	packages := params.(YayInput).Name
	state := params.(YayInput).State
	if state == "absent" {
		return m.RemovePackages(packages, c, runAs)
	} else {
		return m.InstallPackages(packages, c, runAs)
	}
}

func (m YayModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	previousPackages := previous.(YayOutput).Installed
	state := params.(YayInput).State
	if state == "absent" {
		return m.InstallPackages(previousPackages, c, runAs)
	} else {
		return m.RemovePackages(previousPackages, c, runAs)
	}
}

func init() {
	pkg.RegisterModule("yay", YayModule{})
}
