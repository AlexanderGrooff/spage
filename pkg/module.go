package pkg

import (
	"fmt"
	"reflect"
)

// ModuleInput is a marker interface for module input parameters.
// It ensures that module parameters have a method to generate their code representation.
type ConcreteModuleInputProvider interface {
	ToCode() string
	GetVariableUsage() []string
	Validate() error
	// HasRevert indicates if this input defines a revert action.
	HasRevert() bool
	// ProvidesVariables returns a list of variable names this input might define (e.g., keys in set_fact).
	ProvidesVariables() []string
}

// Its primary role is to facilitate correct JSON/YAML marshaling/unmarshaling
// when it's a field within another struct (like Task).
type ModuleInput struct {
	// Actual holds the concrete *ShellInput, *CopyInput, etc.
	// Let the standard marshalers handle this field when marshaling the parent Task struct,
	// using the custom Task MarshalJSON.
	Actual ConcreteModuleInputProvider `json:"actual,omitempty" yaml:"actual,omitempty"`
}

// ToCode delegates to the Actual ConcreteModuleInputProvider.
// It's called during code generation, Actual must be populated.
func (mi *ModuleInput) ToCode() string {
	if mi.Actual == nil {
		// This indicates a programming error if called before Actual is populated.
		panic("ModuleInput.Actual is nil when ToCode() is called")
	}
	return mi.Actual.ToCode()
}

// GetVariableUsage delegates to the Actual ConcreteModuleInputProvider.
func (mi *ModuleInput) GetVariableUsage() []string {
	if mi.Actual == nil {
		panic("ModuleInput.Actual is nil when GetVariableUsage() is called")
	}
	return mi.Actual.GetVariableUsage()
}

// Validate delegates to the Actual ConcreteModuleInputProvider.
func (mi *ModuleInput) Validate() error {
	if mi.Actual == nil {
		panic("ModuleInput.Actual is nil when Validate() is called")
	}
	return mi.Actual.Validate()
}

// HasRevert delegates to the Actual ConcreteModuleInputProvider.
func (mi *ModuleInput) HasRevert() bool {
	if mi.Actual == nil {
		// Default to false if Actual is not populated, or panic as above.
		// For safety, returning false might be less disruptive than panicking here.
		return false
	}
	return mi.Actual.HasRevert()
}

// ProvidesVariables delegates to the Actual ConcreteModuleInputProvider.
func (mi *ModuleInput) ProvidesVariables() []string {
	if mi.Actual == nil {
		// Default to nil or empty slice.
		return nil
	}
	return mi.Actual.ProvidesVariables()
}

// ModuleOutput is a marker interface for module output results.
// It ensures that module results can indicate whether they represent a change.
type ModuleOutput interface {
	Changed() bool
	String() string
}

type RevertableChange[T comparable] struct {
	Before T
	After  T
}

func (r RevertableChange[T]) Changed() bool {
	return r.Before != r.After
}

func (r RevertableChange[T]) DiffOutput() (string, error) {
	before, ok := any(r.Before).(string)
	if !ok {
		return "", fmt.Errorf("cannot generate diff for non-string type %T", r.Before)
	}
	after, ok := any(r.After).(string)
	if !ok {
		return "", fmt.Errorf("cannot generate diff for non-string type %T", r.After)
	}
	return GenerateUnifiedDiff("", before, after)
}

type Module interface {
	InputType() reflect.Type
	OutputType() reflect.Type
	Execute(params ConcreteModuleInputProvider, c *Closure, runAs string) (ModuleOutput, error)
	Revert(params ConcreteModuleInputProvider, c *Closure, previous ModuleOutput, runAs string) (ModuleOutput, error)
	ParameterAliases() map[string]string
}

// ModuleDocProvider is an optional interface that a Module can implement to
// provide additional Markdown documentation (description, examples, notes).
// If implemented, tooling like docgen can surface this content in generated docs.
type ModuleDocProvider interface {
	Doc() string
}

// ParameterDoc carries additional documentation for a single input parameter.
// All fields are optional; tooling will merge these with reflected types and comments.
type ParameterDoc struct {
	Description string   // Human-readable description
	Required    *bool    // Whether the parameter is required (nil = unspecified)
	Default     string   // Default value rendered as text
	Choices     []string // Enumerated valid values, if applicable
}

// ParameterDocsProvider is an optional interface that a Module can implement
// to provide enriched documentation for its input parameters.
// The map key should be the YAML parameter name (e.g., "name", "state").
type ParameterDocsProvider interface {
	ParameterDocs() map[string]ParameterDoc
}

var registeredModules = make(map[string]Module)

// RegisterModule allows modules to register themselves by name.
func RegisterModule(name string, module Module) {
	if _, exists := registeredModules[name]; exists {
		panic(fmt.Sprintf("Module %s already registered", name))
	}
	registeredModules[name] = module
}

// GetModule retrieves a registered module by name.
func GetModule(name string) (Module, bool) {
	module, ok := registeredModules[name]
	return module, ok
}

// ListRegisteredModules returns a copy of the registered modules map.
// This function is useful for tooling like documentation generators that
// need to introspect available modules without mutating the registry.
func ListRegisteredModules() map[string]Module {
	out := make(map[string]Module, len(registeredModules))
	for k, v := range registeredModules {
		out[k] = v
	}
	return out
}
