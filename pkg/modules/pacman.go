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
	return fmt.Sprintf("modules.PacmanInput{Name: []string{%q}}",
		strings.Join(i.Name, ", "),
	)
}

func (i PacmanInput) GetVariableUsage() []string {
	return pkg.GetVariableUsageFromString(strings.Join(i.Name, " "))
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
	return len(o.Installed) > 0
}

func (m PacmanModule) IsPackageInstalled(packageName string, c *pkg.HostContext) bool {
	_, _, err := c.RunCommand(fmt.Sprintf("pacman -Qi %s", packageName))
	return err == nil
}

func (m PacmanModule) InstallPackages(packages []string, c *pkg.HostContext) (PacmanOutput, error) {
	missingPackages := []string{}
	for _, packageName := range packages {
		if !m.IsPackageInstalled(packageName, c) {
			missingPackages = append(missingPackages, packageName)
		}
	}
	if len(missingPackages) > 0 {
		stdout, stderr, err := c.RunCommand(fmt.Sprintf("pacman -S --noconfirm %s", strings.Join(missingPackages, " ")))
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

func (m PacmanModule) RemovePackages(packages []string, c *pkg.HostContext) (PacmanOutput, error) {
	presentPackages := []string{}
	for _, packageName := range packages {
		if m.IsPackageInstalled(packageName, c) {
			presentPackages = append(presentPackages, packageName)
		}
	}
	if len(presentPackages) > 0 {
		stdout, stderr, err := c.RunCommand(fmt.Sprintf("pacman -Rns --noconfirm %s", strings.Join(presentPackages, " ")))
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

func (m PacmanModule) Execute(params pkg.ModuleInput, c *pkg.HostContext) (pkg.ModuleOutput, error) {
	packages := params.(PacmanInput).Name
	state := params.(PacmanInput).State
	if state == "absent" {
		return m.RemovePackages(packages, c)
	} else {
		return m.InstallPackages(packages, c)
	}
}

func (m PacmanModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput) (pkg.ModuleOutput, error) {
	previousPackages := previous.(PacmanOutput).Installed
	state := params.(PacmanInput).State
	if state == "absent" {
		return m.InstallPackages(previousPackages, c)
	} else {
		return m.RemovePackages(previousPackages, c)
	}
}

func init() {
	pkg.RegisterModule("pacman", PacmanModule{})
}
