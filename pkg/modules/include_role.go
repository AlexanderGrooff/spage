package modules

import (
	"fmt"
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg"
)

// IncludeRole meta module: executes its children like a block.

type IncludeRoleModule struct{}

func (m IncludeRoleModule) InputType() reflect.Type             { return reflect.TypeOf(IncludeRoleInput{}) }
func (m IncludeRoleModule) OutputType() reflect.Type            { return reflect.TypeOf(IncludeRoleOutput{}) }
func (m IncludeRoleModule) ParameterAliases() map[string]string { return nil }

type IncludeRoleInput struct {
	Name string `yaml:"name"`
}

func (i IncludeRoleInput) GetVariableUsage() []string {
	return pkg.GetVariableUsageFromTemplate(i.Name)
}
func (i IncludeRoleInput) ProvidesVariables() []string { return []string{} }
func (i IncludeRoleInput) HasRevert() bool             { return false }
func (i IncludeRoleInput) ToCode() string {
	return fmt.Sprintf("modules.IncludeRoleInput{Name: %q}", i.Name)
}
func (i IncludeRoleInput) Validate() error {
	if i.Name == "" {
		return fmt.Errorf("missing 'name' input for include_role directive")
	}
	return nil
}

type IncludeRoleOutput struct{ pkg.ModuleOutput }

func (o IncludeRoleOutput) String() string { return "Included role executed" }

func (m IncludeRoleModule) EvaluateExecute(metatask *pkg.MetaTask, closure *pkg.Closure) chan pkg.TaskResult {
	return BlockModule{}.EvaluateExecute(metatask, closure)
}

func (m IncludeRoleModule) EvaluateRevert(metatask *pkg.MetaTask, closure *pkg.Closure) chan pkg.TaskResult {
	return BlockModule{}.EvaluateRevert(metatask, closure)
}

func init() {
	pkg.RegisterMetaModule("include_role", IncludeRoleModule{})
	pkg.RegisterMetaModule("import_role", IncludeRoleModule{})
}
