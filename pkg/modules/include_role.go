package modules

import (
	"fmt"
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg"
)

// IncludeRoleInput defines the structure for the 'include_role' directive parameters.
// It's used during preprocessing, not as a runtime module.
type IncludeRoleInput struct {
	Name string `yaml:"name"`
	// TODO: Add support for tasks_from, vars, etc. later
}

// InputType returns the type of IncludeRoleInput, used for parsing.
func (i IncludeRoleInput) InputType() reflect.Type {
	return reflect.TypeOf(IncludeRoleInput{})
}

// GetVariableUsage identifies variables used in the role name.
func (i IncludeRoleInput) GetVariableUsage() []string {
	// Check if the role name itself contains any variables
	return pkg.GetVariableUsageFromTemplate(i.Name)
}

// Validate checks if the required 'name' parameter is present.
func (i IncludeRoleInput) Validate() error {
	if i.Name == "" {
		return fmt.Errorf("missing 'name' input for include_role directive")
	}
	// Basic validation: check if the role directory exists (optional but helpful)
	// Assuming a standard roles path relative to the playbook or a known base.
	// This path might need adjustment based on project structure.
	// NOTE: Actual existence check is done during preprocessing.
	// rolePath := filepath.Join("roles", i.Name)
	// if _, err := os.Stat(rolePath); os.IsNotExist(err) {
	// 	 return fmt.Errorf("role directory not found: %s", rolePath)
	// }
	return nil
}

/*
// Original Module implementation - no longer needed as it's handled by preprocessing
type IncludeRoleModule struct{}
func (m IncludeRoleModule) InputType() reflect.Type { ... }
func (m IncludeRoleModule) OutputType() reflect.Type { ... }
type IncludeRoleOutput struct { ... }
func (i IncludeRoleInput) ToCode() string { ... }
func (o IncludeRoleOutput) String() string { ... }
func (o IncludeRoleOutput) Changed() bool { ... }
func (m IncludeRoleModule) Execute(...) (pkg.ModuleOutput, error) { ... }
func (m IncludeRoleModule) Revert(...) (pkg.ModuleOutput, error) { ... }
func init() { pkg.RegisterModule("include_role", IncludeRoleModule{}) }
*/
