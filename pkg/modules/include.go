package modules

import (
	"fmt"
	"os"
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg"
)

type IncludeModule struct{}

func (m IncludeModule) InputType() reflect.Type {
	return reflect.TypeOf(IncludeInput{})
}

func (m IncludeModule) OutputType() reflect.Type {
	return reflect.TypeOf(IncludeOutput{})
}

type IncludeInput struct {
	Path string `yaml:"path"`
	pkg.ModuleInput
}

type IncludeOutput struct {
	Tasks []pkg.Task
	pkg.ModuleOutput
}

func (i IncludeInput) ToCode() string {
	return fmt.Sprintf("modules.IncludeInput{Path: %q}", i.Path)
}

func (i IncludeInput) GetVariableUsage() []string {
	// Check if the path itself contains any variables
	return pkg.GetVariableUsageFromTemplate(i.Path)
}

func (i IncludeInput) Validate() error {
	if i.Path == "" {
		return fmt.Errorf("missing Path input")
	}
	return nil
}

func (o IncludeOutput) String() string {
	return fmt.Sprintf("Included %d tasks", len(o.Tasks))
}

func (o IncludeOutput) Changed() bool {
	return false // Include module doesn't modify anything directly
}

func (m IncludeModule) Execute(params pkg.ModuleInput, c *pkg.HostContext, runAs string) (pkg.ModuleOutput, error) {
	p := params.(IncludeInput)

	// Template the path if it contains variables
	path, err := pkg.TemplateString(p.Path, c.Facts)
	if err != nil {
		return nil, fmt.Errorf("failed to template path %q: %v", p.Path, err)
	}

	// Read and parse the included playbook
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading YAML file: %v", err)
	}
	tasks, err := pkg.TextToTasks([]byte(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse included playbook %s: %v", path, err)
	}

	pkg.DebugOutput("Found %d tasks in included playbook %s", len(tasks), path)
	return IncludeOutput{Tasks: tasks}, nil
}

func (m IncludeModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	// Include module doesn't need to revert anything
	return IncludeOutput{}, nil
}

func init() {
	pkg.RegisterModule("include", IncludeModule{})
}
