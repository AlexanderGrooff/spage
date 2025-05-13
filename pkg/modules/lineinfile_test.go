package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestLineinfileInput_ModuleInputCompatibility(t *testing.T) {
	lineinfileInput := &LineinfileInput{
		Path:   "/tmp/testfile.txt",
		Line:   "Hello, Spage!",
		State:  "present",
		Create: true,
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = lineinfileInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: lineinfileInput}

	// ToCode should not panic and should contain 'LineinfileInput'
	code := mi.ToCode()
	assert.Contains(t, code, "LineinfileInput", "ToCode output should mention LineinfileInput")

	// GetVariableUsage should return a slice
	vars := mi.GetVariableUsage()
	assert.IsType(t, []string{}, vars)

	// Validate should not return error for valid input
	err := mi.Validate()
	assert.NoError(t, err)

	// Check HasRevert implementation
	hasRevert := mi.HasRevert()
	assert.True(t, hasRevert, "HasRevert should be true")

	// ProvidesVariables should return nil or empty
	assert.Nil(t, mi.ProvidesVariables())
}
