package modules

import (
	"fmt"
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

// mockHostContext implements pkg.HostContext
type mockHostContext struct {
	pkg.HostContext
}

func newMockHostContext() *pkg.HostContext {
	return &pkg.HostContext{
		Host:    &pkg.Host{Name: "test", Host: "localhost", IsLocal: true},
		Facts:   make(pkg.Facts),
		History: make(pkg.History),
	}
}

func (m *mockHostContext) RunCommand(command, username string) (string, string, error) {
	// Mock implementation that returns the expected values from test cases
	return "test stdout", "test stderr", nil
}

func (m *mockHostContext) ReadFile(filename, username string) (string, error) {
	return "test file content", nil
}

func (m *mockHostContext) WriteFile(filename, contents, username string) error {
	return nil
}

func TestShellModule_Execute(t *testing.T) {
	tests := []struct {
		name            string
		input           ShellInput
		expectedCommand string
		mockStdout      string
		mockStderr      string
		mockError       error
		wantOutput      ShellOutput
		wantErr         bool
	}{
		{
			name: "successful command",
			input: ShellInput{
				Execute: "echo hello",
			},
			expectedCommand: "echo hello",
			mockStdout:      "hello\n",
			wantOutput: ShellOutput{
				Stdout:  "hello\n",
				Command: "echo hello",
			},
		},
		{
			name: "command with error",
			input: ShellInput{
				Execute: "invalid-command",
			},
			expectedCommand: "invalid-command",
			mockStderr:      "command not found",
			mockError:       fmt.Errorf("exit status 127"),
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := newMockHostContext()

			module := ShellModule{}
			output, err := module.Execute(tt.input, mockCtx, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			shellOutput := output.(ShellOutput)
			assert.Equal(t, tt.wantOutput.Stdout, shellOutput.Stdout)
			assert.Equal(t, tt.wantOutput.Command, shellOutput.Command)
		})
	}
}

func TestShellModule_Revert(t *testing.T) {
	tests := []struct {
		name            string
		input           ShellInput
		expectedCommand string
		mockStdout      string
		mockStderr      string
		mockError       error
		previous        ShellOutput
		wantOutput      ShellOutput
		wantErr         bool
	}{
		{
			name: "successful revert",
			input: ShellInput{
				Revert: "rm -f file.txt",
			},
			expectedCommand: "rm -f file.txt",
			previous: ShellOutput{
				Command: "touch file.txt",
				Stdout:  "",
			},
			wantOutput: ShellOutput{
				Command: "rm -f file.txt",
			},
		},
		// {
		// 	name: "revert with error",
		// 	input: ShellInput{
		// 		Revert: "rm -f nonexistent.txt",
		// 	},
		// 	expectedCommand: "rm -f nonexistent.txt",
		// 	mockStderr:      "no such file or directory",
		// 	mockError:       fmt.Errorf("exit status 1"),
		// 	previous: ShellOutput{
		// 		Command: "touch nonexistent.txt",
		// 		Stdout:  "",
		// 	},
		// 	wantErr: true,
		// },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := newMockHostContext()

			module := ShellModule{}
			output, err := module.Revert(tt.input, mockCtx, tt.previous, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			shellOutput := output.(ShellOutput)
			assert.Equal(t, tt.wantOutput.Command, shellOutput.Command)
		})
	}
}

func TestShellModule_ValidateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   ShellInput
		wantErr bool
	}{
		{
			name: "valid execute command",
			input: ShellInput{
				Execute: "echo hello",
			},
			wantErr: false,
		},
		{
			name: "valid revert command",
			input: ShellInput{
				Revert: "rm -f file.txt",
			},
			wantErr: false,
		},
		{
			name:    "missing both execute and revert",
			input:   ShellInput{},
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
