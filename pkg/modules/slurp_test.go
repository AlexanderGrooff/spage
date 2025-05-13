package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestSlurpInput_ModuleInputCompatibility(t *testing.T) {
	slurpInput := &SlurpInput{
		Source: "/etc/hosts",
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = slurpInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: slurpInput}

	// ToCode should not panic and should contain 'SlurpInput'
	code := mi.ToCode()
	assert.Contains(t, code, "SlurpInput", "ToCode output should mention SlurpInput")

	// GetVariableUsage should return a slice
	vars := mi.GetVariableUsage()
	assert.IsType(t, []string{}, vars)

	// Validate should not return error for valid input
	err := mi.Validate()
	assert.NoError(t, err)

	// Check HasRevert implementation (slurp is read-only, no revert needed)
	hasRevert := mi.HasRevert()
	assert.False(t, hasRevert)

	// ProvidesVariables should return nil or empty
	assert.Nil(t, mi.ProvidesVariables())
}
