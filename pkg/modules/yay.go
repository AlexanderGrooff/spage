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
	return fmt.Sprintf("modules.YayInput{Name: []string{%q}}",
		strings.Join(i.Name, ", "),
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

func IsPackageInstalled(packageName string, c *pkg.HostContext) bool {
	_, _, err := c.RunCommand(fmt.Sprintf("yay -Qi %s", packageName))
	return err == nil
}

func InstallPackages(packages []string, c *pkg.HostContext) (YayOutput, error) {
	missingPackages := []string{}
	for _, packageName := range packages {
		if !IsPackageInstalled(packageName, c) {
			missingPackages = append(missingPackages, packageName)
		}
	}
	if len(missingPackages) > 0 {
		stdout, stderr, err := c.RunCommand(fmt.Sprintf("yay -S --noconfirm %s", strings.Join(missingPackages, " ")))
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

func RemovePackages(packages []string, c *pkg.HostContext) (YayOutput, error) {
	presentPackages := []string{}
	for _, packageName := range packages {
		if IsPackageInstalled(packageName, c) {
			presentPackages = append(presentPackages, packageName)
		}
	}
	if len(presentPackages) > 0 {
		stdout, stderr, err := c.RunCommand(fmt.Sprintf("yay -Rns --noconfirm %s", strings.Join(presentPackages, " ")))
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

func (s YayModule) Execute(params pkg.ModuleInput, c *pkg.HostContext) (pkg.ModuleOutput, error) {
	packages := params.(YayInput).Name
	state := params.(YayInput).State
	if state == "absent" {
		return RemovePackages(packages, c)
	} else {
		return InstallPackages(packages, c)
	}
}

func (s YayModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput) (pkg.ModuleOutput, error) {
	previousPackages := previous.(YayOutput).Installed
	state := params.(YayInput).State
	if state == "absent" {
		return InstallPackages(previousPackages, c)
	} else {
		return RemovePackages(previousPackages, c)
	}
}

func init() {
	pkg.RegisterModule("yay", YayModule{})
}
