package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestYayInput_ModuleInputCompatibility(t *testing.T) {
	yayInput := &YayInput{
		Name:  []string{"git", "neovim"},
		State: "present",
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = yayInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: yayInput}

	// ToCode should not panic and should contain 'YayInput'
	code := mi.ToCode()
	assert.Contains(t, code, "YayInput", "ToCode output should mention YayInput")

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
