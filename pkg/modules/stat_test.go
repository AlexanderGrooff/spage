package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestStatInput_ModuleInputCompatibility(t *testing.T) {
	statInput := &StatInput{
		Path:          "/tmp/test.txt",
		GetChecksum:   true,
		GetAttributes: true,
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = statInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: statInput}

	// ToCode should not panic and should contain 'StatInput'
	code := mi.ToCode()
	assert.Contains(t, code, "StatInput", "ToCode output should mention StatInput")

	// GetVariableUsage should return a slice
	vars := mi.GetVariableUsage()
	assert.IsType(t, []string{}, vars)

	// Validate should not return error for valid input
	err := mi.Validate()
	assert.NoError(t, err)

	// Check HasRevert implementation (stat shouldn't need revert)
	hasRevert := mi.HasRevert()
	assert.False(t, hasRevert)

	// ProvidesVariables should return nil for stat
	providedVars := mi.ProvidesVariables()
	assert.Nil(t, providedVars)
}
