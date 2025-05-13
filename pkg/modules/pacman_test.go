package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestPacmanInput_ModuleInputCompatibility(t *testing.T) {
	pacmanInput := &PacmanInput{
		Name:  []string{"git", "vim"},
		State: "present",
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = pacmanInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: pacmanInput}

	// ToCode should not panic and should contain 'PacmanInput'
	code := mi.ToCode()
	assert.Contains(t, code, "PacmanInput", "ToCode output should mention PacmanInput")

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
