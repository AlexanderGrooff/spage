package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestAnsiblePythonInput_ModuleInputCompatibility(t *testing.T) {
	apt := &AnsiblePythonInput{
		ModuleName: "curl",
		Args:       map[string]any{},
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = apt

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: apt}

	// ToCode should not panic and should contain 'AnsiblePythonInput'
	code := mi.ToCode()
	assert.Contains(t, code, "AnsiblePythonInput", "ToCode output should mention AnsiblePythonInput")

	// GetVariableUsage should return a slice (empty for this input)
	vars := mi.GetVariableUsage()
	assert.IsType(t, []string{}, vars)

	// Validate should not return error for valid input
	err := mi.Validate()
	assert.NoError(t, err)

	// HasRevert should be false for AnsiblePythonInput
	assert.False(t, mi.HasRevert())

	// ProvidesVariables should return nil or empty
	assert.Nil(t, mi.ProvidesVariables())
}

func TestAnsiblePythonOutput_ModuleOutputCompatibility(t *testing.T) {
	output := &AnsiblePythonOutput{
		WasChanged: false,
		Failed:     false,
		Msg:        "",
		Results:    map[string]any{},
	}

	// Ensure it implements FactProvider
	var _ pkg.FactProvider = output

	// AsFacts should return a map
	facts := output.AsFacts()
	assert.IsType(t, map[string]interface{}{}, facts)

	// Changed should return the WasChanged value
	assert.Equal(t, output.WasChanged, output.Changed())
}
