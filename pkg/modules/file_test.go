package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestFileInput_ModuleInputCompatibility(t *testing.T) {
	fileInput := &FileInput{
		Path:  "/tmp/test.txt",
		State: "touch",
		Mode:  "0644",
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = fileInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: fileInput}

	// ToCode should not panic and should contain 'FileInput'
	code := mi.ToCode()
	assert.Contains(t, code, "FileInput", "ToCode output should mention FileInput")

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
