package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestAssertInput_ModuleInputCompatibility(t *testing.T) {
	assertInput := &AssertInput{
		That: []string{"true", "1 == 1"},
		Msg:  "Test assertion",
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = assertInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: assertInput}

	// ToCode should not panic and should contain 'AssertInput'
	code := mi.ToCode()
	assert.Contains(t, code, "AssertInput", "ToCode output should mention AssertInput")

	// GetVariableUsage should return a slice
	vars := mi.GetVariableUsage()
	assert.IsType(t, []string{}, vars)

	// Validate should not return error for valid input
	err := mi.Validate()
	assert.NoError(t, err)

	// Check HasRevert implementation (assertions shouldn't need revert)
	hasRevert := mi.HasRevert()
	assert.False(t, hasRevert)

	// ProvidesVariables should return nil or empty
	assert.Nil(t, mi.ProvidesVariables())
}
