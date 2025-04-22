package modules

import (
	"fmt"
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg"
)

// IncludeInput defines the structure for the 'include' directive parameters.
// It's used during preprocessing, not as a runtime module.
type IncludeInput struct {
	Path string `yaml:"path"`
}

// InputType returns the type of IncludeInput, used for parsing.
func (i IncludeInput) InputType() reflect.Type {
	return reflect.TypeOf(IncludeInput{})
}

// GetVariableUsage identifies variables used in the path, potentially for future templating.
func (i IncludeInput) GetVariableUsage() []string {
	// Check if the path itself contains any variables
	return pkg.GetVariableUsageFromTemplate(i.Path)
}

// Validate checks if the required 'path' parameter is present.
func (i IncludeInput) Validate() error {
	if i.Path == "" {
		return fmt.Errorf("missing Path input for include directive")
	}
	return nil
}

/*
// Original Module implementation - no longer needed as it's handled by preprocessing
type IncludeModule struct{}
func (m IncludeModule) InputType() reflect.Type { ... }
func (m IncludeModule) OutputType() reflect.Type { ... }
type IncludeOutput struct { ... }
func (i IncludeInput) ToCode() string { ... }
func (o IncludeOutput) String() string { ... }
func (o IncludeOutput) Changed() bool { ... }
func (m IncludeModule) Execute(...) (pkg.ModuleOutput, error) { ... }
func (m IncludeModule) Revert(...) (pkg.ModuleOutput, error) { ... }
func init() { pkg.RegisterModule("include", IncludeModule{}) }
*/
