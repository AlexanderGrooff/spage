package modules

import (
	"sync"
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebugInput_ModuleInputCompatibility(t *testing.T) {
	debugInputMsg := &DebugInput{Msg: "Hello"}
	var _ pkg.ConcreteModuleInputProvider = debugInputMsg

	miMsg := &pkg.ModuleInput{Actual: debugInputMsg}
	assert.Contains(t, miMsg.ToCode(), "modules.DebugInput{Msg: \"Hello\"}")
	assert.Empty(t, miMsg.GetVariableUsage())
	assert.NoError(t, miMsg.Validate())
	assert.False(t, miMsg.HasRevert())
	assert.Empty(t, miMsg.ProvidesVariables())

	debugInputVar := &DebugInput{Var: "some_var"}
	var _ pkg.ConcreteModuleInputProvider = debugInputVar

	miVar := &pkg.ModuleInput{Actual: debugInputVar}
	assert.Contains(t, miVar.ToCode(), "modules.DebugInput{Var: \"some_var\"}")
	assert.Empty(t, miVar.GetVariableUsage()) // Direct var name, not a template
	assert.NoError(t, miVar.Validate())

	debugInputVarTemplate := &DebugInput{Var: "{{ var_in_var }}"}
	assert.Equal(t, []string{"var_in_var"}, debugInputVarTemplate.GetVariableUsage())

	debugInputMsgTemplate := &DebugInput{Msg: "Value is {{ my_val }}"}
	assert.Equal(t, []string{"my_val"}, debugInputMsgTemplate.GetVariableUsage())
}

func TestDebugInput_Validate(t *testing.T) {
	tests := []struct {
		name        string
		input       DebugInput
		expectError bool
	}{
		{"valid msg", DebugInput{Msg: "hello"}, false},
		{"valid var", DebugInput{Var: "a_variable"}, false},
		{"both msg and var", DebugInput{Msg: "hello", Var: "a_variable"}, true},
		{"neither msg nor var", DebugInput{}, true},
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

func TestDebugModule_Execute_Msg(t *testing.T) {
	mod := DebugModule{}
	closure := pkg.ConstructClosure(&pkg.HostContext{Host: &pkg.Host{Name: "localhost"}, Facts: &sync.Map{}}, pkg.Task{})
	input := DebugInput{Msg: "Hello, {{ world_var }}!"}
	closure.HostContext.Facts.Store("world_var", "Earth")

	output, err := mod.Execute(input, closure, "")
	require.NoError(t, err)
	require.NotNil(t, output)

	dbgOutput, ok := output.(DebugOutput)
	require.True(t, ok)

	assert.Equal(t, "msg: Hello, Earth!", dbgOutput.MessagePrinted)
	assert.False(t, dbgOutput.Changed())
	assert.Equal(t, "msg: Hello, Earth!", dbgOutput.String())
}

func TestDebugModule_Execute_Var(t *testing.T) {
	mod := DebugModule{}
	closure := pkg.ConstructClosure(&pkg.HostContext{Host: &pkg.Host{Name: "localhost"}, Facts: &sync.Map{}}, pkg.Task{})
	closure.HostContext.Facts.Store("my_fact", "fact_value")
	closure.HostContext.Facts.Store("my_map_fact", map[string]interface{}{"key": "value"})

	tests := []struct {
		name           string
		varName        string
		expectedOutput string
	}{
		{"simple var", "my_fact", "var: my_fact = fact_value"},
		{"map var", "my_map_fact", "var: my_map_fact = map[string]interface {}{\"key\":\"value\"}"},
		{"var not found", "non_existent_var", "var: non_existent_var (not found)"},
		{
			name:           "template in var name",
			varName:        "{{ var_ref }}", // var_ref will resolve to my_fact
			expectedOutput: "var: my_fact = fact_value",
		},
	}

	closure.HostContext.Facts.Store("var_ref", "my_fact") // For the templated var name test

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := DebugInput{Var: tt.varName}
			output, err := mod.Execute(input, closure, "")
			require.NoError(t, err)
			require.NotNil(t, output)

			dbgOutput, ok := output.(DebugOutput)
			require.True(t, ok)
			assert.Equal(t, tt.expectedOutput, dbgOutput.MessagePrinted)
			assert.False(t, dbgOutput.Changed())
		})
	}
}

func TestDebugModule_Revert_Msg(t *testing.T) {
	mod := DebugModule{}
	closure := pkg.ConstructClosure(&pkg.HostContext{Host: &pkg.Host{Name: "localhost"}, Facts: &sync.Map{}}, pkg.Task{})
	input := DebugInput{Msg: "Reverting: {{ world_var }}!"}
	closure.HostContext.Facts.Store("world_var", "Mars")

	// Test revert with no previous output
	output, err := mod.Revert(input, closure, nil, "")
	require.NoError(t, err)
	dbgOutput, ok := output.(DebugOutput)
	require.True(t, ok)
	assert.Equal(t, "msg: Reverting: Mars! [revert]", dbgOutput.MessagePrinted)

	// Test revert with previous output (should still print the new message)
	prevOutput := DebugOutput{MessagePrinted: "previous execution message"}
	output, err = mod.Revert(input, closure, prevOutput, "")
	require.NoError(t, err)
	dbgOutput, ok = output.(DebugOutput)
	require.True(t, ok)
	assert.Equal(t, "msg: Reverting: Mars! [revert]", dbgOutput.MessagePrinted)
}

func TestDebugModule_Revert_Var(t *testing.T) {
	mod := DebugModule{}
	closure := pkg.ConstructClosure(&pkg.HostContext{Host: &pkg.Host{Name: "localhost"}, Facts: &sync.Map{}}, pkg.Task{})
	closure.HostContext.Facts.Store("revert_fact", "revert_value")
	closure.HostContext.Facts.Store("revert_map_fact", map[string]interface{}{"rkey": "rvalue"})

	tests := []struct {
		name           string
		varName        string
		expectedOutput string
	}{
		{"simple var revert", "revert_fact", "var: revert_fact = revert_value [revert]"},
		{"map var revert", "revert_map_fact", "var: revert_map_fact = map[string]interface {}{\"rkey\":\"rvalue\"} [revert]"},
		{"var not found revert", "non_existent_revert_var", "var: non_existent_revert_var (not found) [revert]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := DebugInput{Var: tt.varName}
			output, err := mod.Revert(input, closure, nil, "") // Previous output nil for simplicity here
			require.NoError(t, err)
			dbgOutput, ok := output.(DebugOutput)
			require.True(t, ok)
			assert.Equal(t, tt.expectedOutput, dbgOutput.MessagePrinted)
		})
	}
}

func TestDebugModule_Revert(t *testing.T) {
	mod := DebugModule{}
	closure := pkg.ConstructClosure(&pkg.HostContext{Host: &pkg.Host{Name: "localhost"}, Facts: &sync.Map{}}, pkg.Task{})
	input := DebugInput{Msg: "test"}

	// Revert with no previous output
	output, err := mod.Revert(input, closure, nil, "")
	require.NoError(t, err)
	dbgOutput, ok := output.(DebugOutput)
	require.True(t, ok)
	assert.Equal(t, "msg: test [revert]", dbgOutput.MessagePrinted)

	// Revert with previous output
	prevOutput := DebugOutput{MessagePrinted: "previous state"}
	output, err = mod.Revert(input, closure, prevOutput, "")
	require.NoError(t, err)
	dbgOutput, ok = output.(DebugOutput)
	require.True(t, ok)
	assert.Equal(t, "msg: test [revert]", dbgOutput.MessagePrinted)
}

func init() {
	// Initialize logger for testing to avoid nil panics if common.Log is used directly in modules
	// common.InitLogger("debug", "text", "") // Logger is initialized by default in common package
}
