package modules

import (
	"sync"
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailInput_ModuleInputCompatibility(t *testing.T) {
	failInput := &FailInput{Msg: "Test failure"}
	var _ pkg.ConcreteModuleInputProvider = failInput

	mi := &pkg.ModuleInput{Actual: failInput}
	assert.Contains(t, mi.ToCode(), "modules.FailInput{Msg: \"Test failure\"}")
	assert.Empty(t, mi.GetVariableUsage()) // No template in this specific message
	assert.NoError(t, mi.Validate())
	assert.False(t, mi.HasRevert())
	assert.Empty(t, mi.ProvidesVariables())

	failInputWithTemplate := &FailInput{Msg: "Failed due to {{ reason }}"}
	assert.Equal(t, []string{"reason"}, failInputWithTemplate.GetVariableUsage())
}

func TestFailInput_Validate(t *testing.T) {
	tests := []struct {
		name        string
		input       FailInput
		expectError bool
	}{
		{"valid msg", FailInput{Msg: "An error occurred"}, false},
		{"empty msg", FailInput{Msg: ""}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFailModule_Execute(t *testing.T) {
	mod := FailModule{}
	closure := pkg.ConstructClosure(&pkg.HostContext{Host: &pkg.Host{Name: "localhost"}, Facts: &sync.Map{}}, pkg.Task{})

	t.Run("simple message", func(t *testing.T) {
		input := FailInput{Msg: "Intentional failure for test."}
		output, err := mod.Execute(input, closure, "")

		require.Error(t, err)
		assert.Equal(t, "Intentional failure for test.", err.Error())
		require.NotNil(t, output)
		failOut, ok := output.(FailOutput)
		require.True(t, ok)
		assert.Equal(t, "Intentional failure for test.", failOut.FailedMessage)
		assert.False(t, failOut.Changed())
		assert.Equal(t, "Intentional failure for test.", failOut.String())
	})

	t.Run("templated message", func(t *testing.T) {
		closure.HostContext.Facts.Store("error_reason", "critical system issue")
		input := FailInput{Msg: "Failure due to: {{ error_reason }}"}
		output, err := mod.Execute(input, closure, "")

		require.Error(t, err)
		expectedMsg := "Failure due to: critical system issue"
		assert.Equal(t, expectedMsg, err.Error())
		failOut, _ := output.(FailOutput)
		assert.Equal(t, expectedMsg, failOut.FailedMessage)
	})

	t.Run("failed template in message", func(t *testing.T) {
		input := FailInput{Msg: "Problem with {{ undefined_var }}"}
		output, err := mod.Execute(input, closure, "")
		require.Error(t, err)
		// If undefined_var is treated as empty by TemplateString without error:
		assert.Equal(t, "Problem with ", err.Error())
		failOut, _ := output.(FailOutput)
		assert.Equal(t, "Problem with ", failOut.FailedMessage)
	})
}

func TestFailModule_Revert(t *testing.T) {
	mod := FailModule{}
	closure := pkg.ConstructClosure(&pkg.HostContext{Host: &pkg.Host{Name: "localhost"}, Facts: &sync.Map{}}, pkg.Task{})
	input := FailInput{Msg: "revert test"}

	// Revert with no previous output
	output, err := mod.Revert(input, closure, nil, "")
	require.NoError(t, err)
	failOut, ok := output.(FailOutput)
	require.True(t, ok)
	assert.Equal(t, "(fail module revert - no operation performed)", failOut.FailedMessage)

	// Revert with previous output (which was a FailOutput itself)
	prevOutput := FailOutput{FailedMessage: "original failure msg"}
	output, err = mod.Revert(input, closure, prevOutput, "")
	require.NoError(t, err)
	failOut, ok = output.(FailOutput)
	require.True(t, ok)
	assert.Equal(t, "original failure msg", failOut.FailedMessage) // Should return the original failure message
}
