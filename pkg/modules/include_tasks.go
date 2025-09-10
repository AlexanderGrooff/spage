package modules

import (
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg"
)

// IncludeTasks behaves like a lightweight block: it executes its Children
// sequentially and returns a single aggregated TaskResult, mirroring BlockModule.

type IncludeTasksModule struct{}

func (m IncludeTasksModule) InputType() reflect.Type             { return reflect.TypeOf(IncludeTasksInput{}) }
func (m IncludeTasksModule) OutputType() reflect.Type            { return reflect.TypeOf(IncludeTasksOutput{}) }
func (m IncludeTasksModule) ParameterAliases() map[string]string { return nil }

type IncludeTasksInput struct{}

func (i IncludeTasksInput) GetVariableUsage() []string  { return []string{} }
func (i IncludeTasksInput) ProvidesVariables() []string { return []string{} }
func (i IncludeTasksInput) HasRevert() bool             { return false }
func (i IncludeTasksInput) ToCode() string              { return "modules.IncludeTasksInput{}" }
func (i IncludeTasksInput) Validate() error             { return nil }

type IncludeTasksOutput struct{ pkg.ModuleOutput }

func (o IncludeTasksOutput) String() string { return "Included tasks executed" }

func (m IncludeTasksModule) EvaluateExecute(metatask *pkg.MetaTask, closure *pkg.Closure) chan pkg.TaskResult {
	// Delegate to BlockModule logic for simplicity and consistency
	return BlockModule{}.EvaluateExecute(metatask, closure)
}

func (m IncludeTasksModule) EvaluateRevert(metatask *pkg.MetaTask, closure *pkg.Closure) chan pkg.TaskResult {
	return BlockModule{}.EvaluateRevert(metatask, closure)
}

func init() {
	pkg.RegisterMetaModule("include_tasks", IncludeTasksModule{})
	pkg.RegisterMetaModule("import_tasks", IncludeTasksModule{})
	pkg.RegisterMetaModule("include", IncludeTasksModule{})
	pkg.RegisterMetaModule("import_playbook", IncludeTasksModule{})
}
