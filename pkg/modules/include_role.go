package modules

import (
	"fmt"
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg"
)

// IncludeRole meta module: executes its children like a block.

type IncludeRoleModule struct{}

func (m IncludeRoleModule) InputType() reflect.Type  { return reflect.TypeOf(IncludeRoleInput{}) }
func (m IncludeRoleModule) OutputType() reflect.Type { return reflect.TypeOf(IncludeRoleOutput{}) }

// Doc returns module-level documentation rendered into Markdown.
func (m IncludeRoleModule) Doc() string {
	return `Include and execute all tasks from a specified role. This allows you to reuse role functionality within a playbook.

## Examples

` + "```yaml" + `
- name: Include webserver role
  include_role:
    name: webserver

- name: Include role with variables
  include_role:
    name: database
  vars:
    db_name: myapp
    db_user: appuser

- name: Conditionally include role
  include_role:
    name: ssl_certificates
  when: enable_ssl | bool
` + "```" + `

**Note**: The role must be available in the roles directory or in the Ansible roles path.
`
}

// ParameterDocs provides rich documentation for include_role module inputs.
func (m IncludeRoleModule) ParameterDocs() map[string]pkg.ParameterDoc {
	notRequired := false
	return map[string]pkg.ParameterDoc{
		"name": {
			Description: "Name of the role to include and execute.",
			Required:    &notRequired,
			Default:     "",
		},
	}
}
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
