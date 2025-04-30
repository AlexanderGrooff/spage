package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/google/go-cmp/cmp"
)

// SetFactModule implements the logic for setting facts.
type SetFactModule struct{}

func (m SetFactModule) InputType() reflect.Type {
	return reflect.TypeOf(SetFactInput{})
}

func (m SetFactModule) OutputType() reflect.Type {
	return reflect.TypeOf(SetFactOutput{})
}

// SetFactInput defines the structure for facts to be set.
// Using a map directly allows flexible key-value pairs.
type SetFactInput struct {
	Facts map[string]interface{}
	// No longer embedding interface, relying on duck typing or explicit interface implementation if needed
}

// SetFactOutput provides information about the facts that were set.
type SetFactOutput struct {
	FactsSet map[string]interface{} // Record which facts were actually set/modified
	// No longer embedding interface
}

// ToCode generates Go code representation of the SetFactInput.
func (i SetFactInput) ToCode() string {
	var builder strings.Builder
	builder.WriteString("modules.SetFactInput{Facts: map[string]interface{}{")
	count := 0
	for k, v := range i.Facts {
		// Represent value based on its type for better code generation
		var valStr string
		switch vt := v.(type) {
		case string:
			valStr = fmt.Sprintf("%q", vt)
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			valStr = fmt.Sprintf("%v", vt)
		case bool:
			valStr = fmt.Sprintf("%t", vt)
		// Add more types as needed (e.g., slices, maps)
		default:
			// Fallback for complex types - might need more robust serialization
			valStr = fmt.Sprintf("%#v", vt) // Use %#v for a more detailed representation
		}
		builder.WriteString(fmt.Sprintf("%q: %s", k, valStr))
		if count < len(i.Facts)-1 {
			builder.WriteString(", ")
		}
		count++
	}
	builder.WriteString("}}")
	return builder.String()
}

// GetVariableUsage extracts variables used within the fact values.
func (i SetFactInput) GetVariableUsage() []string {
	var vars []string
	for _, v := range i.Facts {
		if strVal, ok := v.(string); ok {
			vars = append(vars, pkg.GetVariableUsageFromTemplate(strVal)...)
		}
		// Potentially recurse into nested maps/slices if needed
	}
	// Deduplicate variables
	uniqueVars := make(map[string]struct{})
	for _, v := range vars {
		uniqueVars[v] = struct{}{}
	}
	result := make([]string, 0, len(uniqueVars))
	for k := range uniqueVars {
		result = append(result, k)
	}
	return result
}

// Validate ensures that there are facts to set.
func (i SetFactInput) Validate() error {
	if len(i.Facts) == 0 {
		return fmt.Errorf("no facts provided to set_fact module")
	}
	return nil
}

// HasRevert indicates whether the input parameters define a revert action.
// For set_fact, revert is handled generically (no-op), so input doesn't define it.
func (i SetFactInput) HasRevert() bool {
	return false
}

// String provides a human-readable summary of the output.
func (o SetFactOutput) String() string {
	if len(o.FactsSet) > 0 {
		// Maybe list the keys set for more detail?
		return fmt.Sprintf("Set %d fact(s)", len(o.FactsSet))
	}
	return "No facts were set or changed."
}

// Changed indicates if any facts were added or modified.
func (o SetFactOutput) Changed() bool {
	return len(o.FactsSet) > 0
}

// UnmarshalYAML custom unmarshaler to handle direct map structure
func (i *SetFactInput) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try unmarshalling into a map first
	factsMap := make(map[string]interface{})
	if err := unmarshal(&factsMap); err == nil {
		// Successfully unmarshalled into a map, assign it
		i.Facts = factsMap
		return nil
	}
	// If that fails, maybe it's structured differently? Add more logic if needed.
	// For now, return the original error
	return fmt.Errorf("failed to unmarshal set_fact input: expected a map")
}

// Execute sets the facts in the host context.
func (m SetFactModule) Execute(params pkg.ModuleInput, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	input, ok := params.(SetFactInput)
	if !ok {
		return nil, fmt.Errorf("invalid params type (%T) for set_fact module", params)
	}

	output := SetFactOutput{FactsSet: make(map[string]interface{})}
	changed := false

	for key, value := range input.Facts {
		var finalValue interface{}
		var err error

		// Attempt to template string values
		if strValue, ok := value.(string); ok {
			finalValue, err = pkg.TemplateString(strValue, closure)
			if err != nil {
				common.LogWarn("Failed to template value for fact, using raw value", map[string]interface{}{
					"host":      closure.HostContext.Host.Name,
					"fact":      key,
					"raw_value": strValue,
					"error":     err.Error(),
				})
				// Use the raw string value if templating fails
				finalValue = strValue
				err = nil // Clear error for this specific case
			}
		} else {
			// For non-string types, use the value directly
			// TODO: Consider templating within nested structures if required
			finalValue = value
		}

		if err != nil { // Check for errors from potential future complex templating
			return output, fmt.Errorf("error processing fact %q: %w", key, err)
		}

		// Check if the fact already exists and if the value has changed using sync.Map methods
		existingValue, exists := closure.HostContext.Facts.Load(key)
		if !exists || !cmp.Equal(existingValue, finalValue) {
			common.DebugOutput("Setting fact %q = %v (was: %v, exists: %t)", key, finalValue, existingValue, exists)
			closure.HostContext.Facts.Store(key, finalValue)    // Use Store to set/update the value
			output.FactsSet[key] = finalValue // Record the fact that was set/changed
			changed = true
		} else {
			common.DebugOutput("Fact %q already set to %v, skipping.", key, finalValue)
		}
	}

	// The conceptual "change" for set_fact happens if any fact was added or its value modified.
	// The Changed() method on the output struct reflects this.
	if !changed {
		common.DebugOutput("No facts were changed by set_fact.")
	}

	return output, nil
}

// Revert for set_fact is generally a no-op. Facts set are part of the context state.
// Undoing them would require tracking the previous state, which adds complexity.
// If a task fails, subsequent tasks won't see the facts set by the failed task anyway.
func (m SetFactModule) Revert(params pkg.ModuleInput, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	common.DebugOutput("Revert called for set_fact module (no-op)")
	// No state to revert directly.
	return SetFactOutput{FactsSet: map[string]interface{}{}}, nil
}

func init() {
	pkg.RegisterModule("set_fact", SetFactModule{})
}

// ParameterAliases defines aliases if needed.
func (m SetFactModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for set_fact
}
