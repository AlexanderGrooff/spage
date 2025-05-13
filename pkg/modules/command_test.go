package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestCommandInput_ModuleInputCompatibility(t *testing.T) {
	commandInput := &CommandInput{
		Execute: "echo 'hello world'",
		Revert:  "echo 'goodbye world'",
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = commandInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: commandInput}

	// ToCode should not panic and should contain 'CommandInput'
	code := mi.ToCode()
	assert.Contains(t, code, "CommandInput", "ToCode output should mention CommandInput")

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
