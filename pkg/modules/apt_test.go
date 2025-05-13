package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestAptInput_ModuleInputCompatibility(t *testing.T) {
	apt := &AptInput{
		Name:        "curl",
		State:       "present",
		UpdateCache: false,
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = apt

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: apt}

	// ToCode should not panic and should contain 'AptInput'
	code := mi.ToCode()
	assert.Contains(t, code, "AptInput", "ToCode output should mention AptInput")

	// GetVariableUsage should return a slice (empty for this input)
	vars := mi.GetVariableUsage()
	assert.IsType(t, []string{}, vars)

	// Validate should not return error for valid input
	err := mi.Validate()
	assert.NoError(t, err)

	// HasRevert should be true for present/absent with a name
	assert.True(t, mi.HasRevert())

	// ProvidesVariables should return nil or empty
	assert.Nil(t, mi.ProvidesVariables())
}
