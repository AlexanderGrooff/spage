package modules

import (
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg"
)

// IncludeTasks behaves like a lightweight block: it executes its Children
// sequentially and returns a single aggregated TaskResult, mirroring BlockModule.

type IncludeTasksModule struct{}

func (m IncludeTasksModule) InputType() reflect.Type  { return reflect.TypeOf(IncludeTasksInput{}) }
func (m IncludeTasksModule) OutputType() reflect.Type { return reflect.TypeOf(IncludeTasksOutput{}) }

// Doc returns module-level documentation rendered into Markdown.
func (m IncludeTasksModule) Doc() string {
	return `Include and execute tasks from external task files. This allows you to organize and reuse task definitions across playbooks.

## Examples

` + "```yaml" + `
- name: Include common setup tasks
  include_tasks: common/setup.yml

- name: Include tasks with variables
  include_tasks: database/install.yml
  vars:
    db_version: "13"

- name: Conditionally include tasks
  include_tasks: ssl/configure.yml
  when: enable_ssl | bool

- name: Import tasks (static inclusion)
  import_tasks: handlers/restart.yml
` + "```" + `

**Note**: include_tasks is dynamic (evaluated at runtime), while import_tasks is static (evaluated at parse time). This module also handles import_playbook and include directives.
`
}

// ParameterDocs provides rich documentation for include_tasks module inputs.
func (m IncludeTasksModule) ParameterDocs() map[string]pkg.ParameterDoc {
	// include_tasks doesn't have traditional parameters - the file path is specified directly
	return map[string]pkg.ParameterDoc{}
}
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
