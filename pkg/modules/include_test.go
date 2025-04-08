package modules

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIncludeModule_Execute(t *testing.T) {
	tests := []struct {
		name       string
		input      IncludeInput
		wantOutput IncludeOutput
		wantErr    bool
	}{
		{
			name: "execute should always error",
			input: IncludeInput{
				Path: "tasks/test.yaml",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := newMockHostContext()

			module := IncludeModule{}
			output, err := module.Execute(tt.input, mockCtx, "")

			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, "include module should not be executed", err.Error())
				return
			}

			assert.NoError(t, err)
			includeOutput := output.(IncludeOutput)
			assert.Equal(t, tt.wantOutput, includeOutput)
		})
	}
}

func TestIncludeModule_Revert(t *testing.T) {
	tests := []struct {
		name       string
		input      IncludeInput
		previous   IncludeOutput
		wantOutput IncludeOutput
		wantErr    bool
	}{
		{
			name: "revert should always succeed",
			input: IncludeInput{
				Path: "tasks/test.yaml",
			},
			previous:   IncludeOutput{},
			wantOutput: IncludeOutput{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := newMockHostContext()

			module := IncludeModule{}
			output, err := module.Revert(tt.input, mockCtx, tt.previous, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			includeOutput := output.(IncludeOutput)
			assert.Equal(t, tt.wantOutput, includeOutput)
		})
	}
}

func TestIncludeModule_ValidateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   IncludeInput
		wantErr bool
	}{
		{
			name: "valid input",
			input: IncludeInput{
				Path: "tasks/test.yaml",
			},
			wantErr: false,
		},
		{
			name: "missing path",
			input: IncludeInput{
				Path: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIncludeOutput_Changed(t *testing.T) {
	output := IncludeOutput{}
	assert.False(t, output.Changed())
}

func TestIncludeOutput_String(t *testing.T) {
	output := IncludeOutput{}
	assert.Equal(t, "Include processed during compilation", output.String())
}

func TestIncludeInput_GetVariableUsage(t *testing.T) {
	tests := []struct {
		name     string
		input    IncludeInput
		wantVars []string
	}{
		{
			name: "path with no variables",
			input: IncludeInput{
				Path: "tasks/test.yaml",
			},
			wantVars: []string(nil),
		},
		{
			name: "path with variables",
			input: IncludeInput{
				Path: "tasks/{{ env }}/test.yaml",
			},
			wantVars: []string{"env"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := tt.input.GetVariableUsage()
			assert.Equal(t, tt.wantVars, vars)
		})
	}
}
