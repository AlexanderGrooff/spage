package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestShellInput_ModuleInputCompatibility(t *testing.T) {
	shellInput := &ShellInput{
		Execute: "echo 'hello world'",
		Revert:  "echo 'reverting'",
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = shellInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: shellInput}

	// ToCode should not panic and should contain 'ShellInput'
	code := mi.ToCode()
	assert.Contains(t, code, "ShellInput", "ToCode output should mention ShellInput")

	// GetVariableUsage should return a slice
	vars := mi.GetVariableUsage()
	assert.IsType(t, []string{}, vars)

	// Validate should not return error for valid input
	err := mi.Validate()
	assert.NoError(t, err)

	// Check HasRevert implementation
	hasRevert := mi.HasRevert()
	assert.True(t, hasRevert)

	// ProvidesVariables should return nil or empty
	assert.Nil(t, mi.ProvidesVariables())
}
