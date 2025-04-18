package modules

import (
	"fmt"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
)

type FileModule struct{}

func (fm FileModule) InputType() reflect.Type {
	return reflect.TypeOf(FileInput{})
}

func (fm FileModule) OutputType() reflect.Type {
	return reflect.TypeOf(FileOutput{})
}

type FileInput struct {
	Path  string `yaml:"path"`
	State string `yaml:"state"`
	Mode  string `yaml:"mode"`
	pkg.ModuleInput
}

type FileOutput struct {
	State pkg.RevertableChange[string]
	Mode  pkg.RevertableChange[string]
	pkg.ModuleOutput
}

func (i FileInput) ToCode() string {
	return fmt.Sprintf("modules.FileInput{Path: %q, State: %q, Mode: %q}",
		i.Path,
		i.State,
		i.Mode,
	)
}

func (i FileInput) GetVariableUsage() []string {
	return pkg.GetVariableUsageFromTemplate(i.Path)
}

func (i FileInput) Validate() error {
	if i.Path == "" {
		return fmt.Errorf("missing Path input")
	}
	if i.State != "" && i.State != "touch" && i.State != "directory" && i.State != "absent" {
		return fmt.Errorf("invalid State value: %s (must be one of: touch, directory, absent)", i.State)
	}
	return nil
}

func (o FileOutput) String() string {
	return fmt.Sprintf("  original state: %q, mode: %q\n  new state: %q, mode: %q",
		o.State.Before, o.Mode.Before,
		o.State.After, o.Mode.After)
}

func (o FileOutput) Changed() bool {
	return o.State.Changed() || o.Mode.Changed()
}

func (m FileModule) Execute(params pkg.ModuleInput, c *pkg.HostContext, runAs string) (pkg.ModuleOutput, error) {
	p := params.(FileInput)

	// Get original state
	contents, err := c.ReadFile(p.Path, runAs)
	originalState := "absent"
	originalMode := ""
	if err == nil {
		// File exists, check if it's a directory
		if strings.Contains(contents, "cannot read") && strings.Contains(contents, "Is a directory") {
			originalState = "directory"
		} else {
			originalState = "file"
		}
		// Get mode using ls command
		stdout, _, err := c.RunCommand(fmt.Sprintf("ls -l %s | cut -d ' ' -f 1", p.Path), runAs)
		if err == nil && len(stdout) >= 4 {
			// ls -l output format: -rwxr-xr-x
			// We want the "rwx" part, which starts at index 1
			originalMode = stdout[1:4]
		}
	}

	// Apply state changes
	switch p.State {
	case "touch":
		if err := c.WriteFile(p.Path, "", runAs); err != nil {
			return nil, fmt.Errorf("failed to touch file %s: %v", p.Path, err)
		}
	case "directory":
		if _, _, err := c.RunCommand(fmt.Sprintf("mkdir -p %s", p.Path), runAs); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %v", p.Path, err)
		}
	case "absent":
		if _, _, err := c.RunCommand(fmt.Sprintf("rm -rf %s", p.Path), runAs); err != nil {
			return nil, fmt.Errorf("failed to remove %s: %v", p.Path, err)
		}
	}

	// Apply mode changes if specified
	newMode := originalMode
	if p.Mode != "" {
		// Convert symbolic mode (rw-) to numeric mode (644)
		modeCmd := p.Mode
		if len(p.Mode) == 3 && !strings.ContainsAny(p.Mode, "0123456789") {
			var numeric int
			for i, c := range p.Mode {
				if c == 'r' {
					numeric |= 4 << (uint(2-i) * 3)
				}
				if c == 'w' {
					numeric |= 2 << (uint(2-i) * 3)
				}
				if c == 'x' {
					numeric |= 1 << (uint(2-i) * 3)
				}
			}
			modeCmd = fmt.Sprintf("%03o", numeric)
		}

		if _, _, err := c.RunCommand(fmt.Sprintf("chmod %s %s", modeCmd, p.Path), runAs); err != nil {
			return nil, fmt.Errorf("failed to chmod %s: %v", p.Path, err)
		}
		newMode = p.Mode
	}

	// Get new state
	newState := originalState
	if p.State != "" {
		newState = p.State
		if p.State == "touch" {
			newState = "file"
		}
	}

	return FileOutput{
		State: pkg.RevertableChange[string]{
			Before: originalState,
			After:  newState,
		},
		Mode: pkg.RevertableChange[string]{
			Before: originalMode,
			After:  newMode,
		},
	}, nil
}

func (m FileModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	p := params.(FileInput)
	if previous == nil {
		common.DebugOutput("Not reverting because previous result was nil")
		return FileOutput{}, nil
	}

	prev := previous.(FileOutput)
	if !prev.Changed() {
		return FileOutput{}, nil
	}

	// Revert state
	switch prev.State.Before {
	case "absent":
		if _, _, err := c.RunCommand(fmt.Sprintf("rm -rf %s", p.Path), runAs); err != nil {
			return nil, fmt.Errorf("failed to remove %s: %v", p.Path, err)
		}
	case "directory":
		if _, _, err := c.RunCommand(fmt.Sprintf("mkdir -p %s", p.Path), runAs); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %v", p.Path, err)
		}
	case "file":
		if err := c.WriteFile(p.Path, "", runAs); err != nil {
			return nil, fmt.Errorf("failed to create file %s: %v", p.Path, err)
		}
	}

	// Revert mode
	if prev.Mode.Before != "" {
		if _, _, err := c.RunCommand(fmt.Sprintf("chmod %s %s", prev.Mode.Before, p.Path), runAs); err != nil {
			return nil, fmt.Errorf("failed to chmod %s: %v", p.Path, err)
		}
	}

	// Flip the before and after values
	return FileOutput{
		State: pkg.RevertableChange[string]{
			Before: prev.State.After,
			After:  prev.State.Before,
		},
		Mode: pkg.RevertableChange[string]{
			Before: prev.Mode.After,
			After:  prev.Mode.Before,
		},
	}, nil
}

func init() {
	pkg.RegisterModule("file", FileModule{})
}
