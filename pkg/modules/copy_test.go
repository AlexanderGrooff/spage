package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestCopyInput_ModuleInputCompatibility(t *testing.T) {
	copyInput := &CopyInput{
		Src:     "/tmp/source.txt",
		Dst:     "/tmp/destination.txt",
		Mode:    "0644",
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = copyInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: copyInput}

	// ToCode should not panic and should contain 'CopyInput'
	code := mi.ToCode()
	assert.Contains(t, code, "CopyInput", "ToCode output should mention CopyInput")

	// GetVariableUsage should return a slice
	vars := mi.GetVariableUsage()
	assert.IsType(t, []string{}, vars)

	// Validate should not return error for valid input
	err := mi.Validate()
	assert.NoError(t, err)

	// Check HasRevert implementation
	hasRevert := mi.HasRevert()
	assert.IsType(t, true, hasRevert)

	// ProvidesVariables should return nil or empty
	assert.Nil(t, mi.ProvidesVariables())
}
