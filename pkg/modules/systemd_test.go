package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestSystemdInput_ModuleInputCompatibility(t *testing.T) {
	systemdInput := &SystemdInput{
		Name:         "nginx",
		State:        "started",
		Enabled:      true,
		DaemonReload: true,
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = systemdInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: systemdInput}

	// ToCode should not panic and should contain 'SystemdInput'
	code := mi.ToCode()
	assert.Contains(t, code, "SystemdInput", "ToCode output should mention SystemdInput")

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
