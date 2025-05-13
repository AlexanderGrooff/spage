package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestSetFactInput_ModuleInputCompatibility(t *testing.T) {
	// Create a SetFactInput with a simple fact
	setFactInput := &SetFactInput{
		Facts: map[string]interface{}{
			"test_fact": "test_value",
		},
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = setFactInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: setFactInput}

	// ToCode should not panic and should contain 'SetFactInput'
	code := mi.ToCode()
	assert.Contains(t, code, "SetFactInput", "ToCode output should mention SetFactInput")

	// GetVariableUsage should return a slice
	vars := mi.GetVariableUsage()
	assert.IsType(t, []string{}, vars)

	// Validate should not return error for valid input
	err := mi.Validate()
	assert.NoError(t, err)

	// Check HasRevert implementation
	hasRevert := mi.HasRevert()
	assert.IsType(t, false, hasRevert)

	// ProvidesVariables should return list of fact keys
	providedVars := mi.ProvidesVariables()
	assert.Contains(t, providedVars, "test_fact")
}
