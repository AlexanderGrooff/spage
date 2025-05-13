package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestGitInput_ModuleInputCompatibility(t *testing.T) {
	gitInput := &GitInput{
		Repo:    "https://github.com/example/repo",
		Dest:    "/tmp/repo",
		Version: "main",
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = gitInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: gitInput}

	// ToCode should not panic and should contain 'GitInput'
	code := mi.ToCode()
	assert.Contains(t, code, "GitInput", "ToCode output should mention GitInput")

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
