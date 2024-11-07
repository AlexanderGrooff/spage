package modules

import (
	"fmt"
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
	return "Include processed during compilation"
}

func (o IncludeOutput) Changed() bool {
	return false
}

func (m IncludeModule) Execute(params pkg.ModuleInput, c *pkg.HostContext, runAs string) (pkg.ModuleOutput, error) {
	// Include is handled during compilation, so this should never be called
	return IncludeOutput{}, fmt.Errorf("include module should not be executed")
}

func (m IncludeModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	return IncludeOutput{}, nil
}

func init() {
	pkg.RegisterModule("include", IncludeModule{})
}
