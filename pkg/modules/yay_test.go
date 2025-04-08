package modules

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestYayModule_Execute(t *testing.T) {
	tests := []struct {
		name       string
		input      YayInput
		mockOutput map[string]struct {
			stdout string
			stderr string
			err    error
		}
		wantOutput YayOutput
		wantErr    bool
	}{
		{
			name: "install new packages",
			input: YayInput{
				Name:  []string{"package1", "package2"},
				State: "present",
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"yay -Qi package1": {
					err: fmt.Errorf("package not found"),
				},
				"yay -Qi package2": {
					err: fmt.Errorf("package not found"),
				},
				"yay -S --noconfirm package1 package2": {
					stdout: "installing packages...",
					stderr: "",
				},
			},
			wantOutput: YayOutput{
				Installed: []string{"package1", "package2"},
				Removed:   nil,
				Stdout:    "installing packages...",
				Stderr:    "",
			},
		},
		{
			name: "remove existing packages",
			input: YayInput{
				Name:  []string{"package1", "package2"},
				State: "absent",
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"yay -Qi package1": {
					stdout: "package info",
				},
				"yay -Qi package2": {
					stdout: "package info",
				},
				"yay -Rns --noconfirm package1 package2": {
					stdout: "removing packages...",
					stderr: "",
				},
			},
			wantOutput: YayOutput{
				Installed: nil,
				Removed:   []string{"package1", "package2"},
				Stdout:    "removing packages...",
				Stderr:    "",
			},
		},
		{
			name: "install already installed packages",
			input: YayInput{
				Name:  []string{"package1", "package2"},
				State: "present",
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"yay -Qi package1": {
					stdout: "package info",
				},
				"yay -Qi package2": {
					stdout: "package info",
				},
			},
			wantOutput: YayOutput{},
		},
		{
			name: "installation failure",
			input: YayInput{
				Name:  []string{"package1"},
				State: "present",
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"yay -Qi package1": {
					err: fmt.Errorf("package not found"),
				},
				"yay -S --noconfirm package1": {
					stderr: "error: target not found: package1",
					err:    fmt.Errorf("installation failed"),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := newMockHostContext()

			module := YayModule{}
			output, err := module.Execute(tt.input, mockCtx, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			yayOutput := output.(YayOutput)
			assert.Equal(t, tt.wantOutput.Installed, yayOutput.Installed)
			assert.Equal(t, tt.wantOutput.Removed, yayOutput.Removed)
			assert.Equal(t, tt.wantOutput.Stdout, yayOutput.Stdout)
			assert.Equal(t, tt.wantOutput.Stderr, yayOutput.Stderr)
		})
	}
}

func TestYayModule_Revert(t *testing.T) {
	tests := []struct {
		name       string
		input      YayInput
		previous   YayOutput
		mockOutput map[string]struct {
			stdout string
			stderr string
			err    error
		}
		wantOutput YayOutput
		wantErr    bool
	}{
		{
			name: "revert installation",
			input: YayInput{
				Name:  []string{"package1", "package2"},
				State: "present",
			},
			previous: YayOutput{
				Installed: []string{"package1", "package2"},
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"yay -Qi package1": {
					stdout: "package info",
				},
				"yay -Qi package2": {
					stdout: "package info",
				},
				"yay -Rns --noconfirm package1 package2": {
					stdout: "removing packages...",
					stderr: "",
				},
			},
			wantOutput: YayOutput{
				Installed: nil,
				Removed:   []string{"package1", "package2"},
				Stdout:    "removing packages...",
				Stderr:    "",
			},
		},
		{
			name: "revert removal",
			input: YayInput{
				Name:  []string{"package1", "package2"},
				State: "absent",
			},
			previous: YayOutput{
				Removed: []string{"package1", "package2"},
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"yay -Qi package1": {
					err: fmt.Errorf("package not found"),
				},
				"yay -Qi package2": {
					err: fmt.Errorf("package not found"),
				},
				"yay -S --noconfirm package1 package2": {
					stdout: "installing packages...",
					stderr: "",
				},
			},
			wantOutput: YayOutput{
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

			module := YayModule{}
			output, err := module.Revert(tt.input, mockCtx, tt.previous, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			yayOutput := output.(YayOutput)
			assert.Equal(t, tt.wantOutput.Installed, yayOutput.Installed)
			assert.Equal(t, tt.wantOutput.Removed, yayOutput.Removed)
			assert.Equal(t, tt.wantOutput.Stdout, yayOutput.Stdout)
			assert.Equal(t, tt.wantOutput.Stderr, yayOutput.Stderr)
		})
	}
}

func TestYayModule_ValidateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   YayInput
		wantErr bool
	}{
		{
			name: "valid input with multiple packages",
			input: YayInput{
				Name:  []string{"package1", "package2"},
				State: "present",
			},
			wantErr: false,
		},
		{
			name: "valid input with single package",
			input: YayInput{
				Name:  []string{"package1"},
				State: "absent",
			},
			wantErr: false,
		},
		{
			name: "missing packages",
			input: YayInput{
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
