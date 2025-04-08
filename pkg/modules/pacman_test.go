package modules

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPacmanModule_Execute(t *testing.T) {
	tests := []struct {
		name       string
		input      PacmanInput
		mockOutput map[string]struct {
			stdout string
			stderr string
			err    error
		}
		wantOutput PacmanOutput
		wantErr    bool
	}{
		{
			name: "install new packages",
			input: PacmanInput{
				Name:  []string{"package1", "package2"},
				State: "present",
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"pacman -Qi package1": {
					err: fmt.Errorf("package not found"),
				},
				"pacman -Qi package2": {
					err: fmt.Errorf("package not found"),
				},
				"pacman -S --noconfirm package1 package2": {
					stdout: "installing packages...",
					stderr: "",
				},
			},
			wantOutput: PacmanOutput{
				Installed: []string{"package1", "package2"},
				Removed:   nil,
				Stdout:    "installing packages...",
				Stderr:    "",
			},
		},
		{
			name: "remove existing packages",
			input: PacmanInput{
				Name:  []string{"package1", "package2"},
				State: "absent",
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"pacman -Qi package1": {
					stdout: "package info",
				},
				"pacman -Qi package2": {
					stdout: "package info",
				},
				"pacman -Rns --noconfirm package1 package2": {
					stdout: "removing packages...",
					stderr: "",
				},
			},
			wantOutput: PacmanOutput{
				Installed: nil,
				Removed:   []string{"package1", "package2"},
				Stdout:    "removing packages...",
				Stderr:    "",
			},
		},
		{
			name: "install already installed packages",
			input: PacmanInput{
				Name:  []string{"package1", "package2"},
				State: "present",
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"pacman -Qi package1": {
					stdout: "package info",
				},
				"pacman -Qi package2": {
					stdout: "package info",
				},
			},
			wantOutput: PacmanOutput{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := newMockHostContext()

			module := PacmanModule{}
			output, err := module.Execute(tt.input, mockCtx, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			pacmanOutput := output.(PacmanOutput)
			assert.Equal(t, tt.wantOutput.Installed, pacmanOutput.Installed)
			assert.Equal(t, tt.wantOutput.Removed, pacmanOutput.Removed)
			assert.Equal(t, tt.wantOutput.Stdout, pacmanOutput.Stdout)
			assert.Equal(t, tt.wantOutput.Stderr, pacmanOutput.Stderr)
		})
	}
}

func TestPacmanModule_Revert(t *testing.T) {
	tests := []struct {
		name       string
		input      PacmanInput
		previous   PacmanOutput
		mockOutput map[string]struct {
			stdout string
			stderr string
			err    error
		}
		wantOutput PacmanOutput
		wantErr    bool
	}{
		{
			name: "revert installation",
			input: PacmanInput{
				Name:  []string{"package1", "package2"},
				State: "present",
			},
			previous: PacmanOutput{
				Installed: []string{"package1", "package2"},
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"pacman -Qi package1": {
					stdout: "package info",
				},
				"pacman -Qi package2": {
					stdout: "package info",
				},
				"pacman -Rns --noconfirm package1 package2": {
					stdout: "removing packages...",
					stderr: "",
				},
			},
			wantOutput: PacmanOutput{
				Installed: nil,
				Removed:   []string{"package1", "package2"},
				Stdout:    "removing packages...",
				Stderr:    "",
			},
		},
		{
			name: "revert removal",
			input: PacmanInput{
				Name:  []string{"package1", "package2"},
				State: "absent",
			},
			previous: PacmanOutput{
				Removed: []string{"package1", "package2"},
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"pacman -Qi package1": {
					err: fmt.Errorf("package not found"),
				},
				"pacman -Qi package2": {
					err: fmt.Errorf("package not found"),
				},
				"pacman -S --noconfirm package1 package2": {
					stdout: "installing packages...",
					stderr: "",
				},
			},
			wantOutput: PacmanOutput{
				Installed: []string{"package1", "package2"},
				Removed:   nil,
				Stdout:    "installing packages...",
				Stderr:    "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := newMockHostContext()

			module := PacmanModule{}
			output, err := module.Revert(tt.input, mockCtx, tt.previous, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			pacmanOutput := output.(PacmanOutput)
			assert.Equal(t, tt.wantOutput.Installed, pacmanOutput.Installed)
			assert.Equal(t, tt.wantOutput.Removed, pacmanOutput.Removed)
			assert.Equal(t, tt.wantOutput.Stdout, pacmanOutput.Stdout)
			assert.Equal(t, tt.wantOutput.Stderr, pacmanOutput.Stderr)
		})
	}
}

func TestPacmanModule_ValidateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   PacmanInput
		wantErr bool
	}{
		{
			name: "valid input with multiple packages",
			input: PacmanInput{
				Name:  []string{"package1", "package2"},
				State: "present",
			},
			wantErr: false,
		},
		{
			name: "valid input with single package",
			input: PacmanInput{
				Name:  []string{"package1"},
				State: "absent",
			},
			wantErr: false,
		},
		{
			name: "missing packages",
			input: PacmanInput{
				Name:  []string{},
				State: "present",
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
