package modules

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGitModule_Execute(t *testing.T) {
	tests := []struct {
		name       string
		input      GitInput
		mockOutput map[string]struct {
			stdout string
			stderr string
			err    error
		}
		wantOutput GitOutput
		wantErr    bool
	}{
		{
			name: "successful clone and checkout",
			input: GitInput{
				Repo:    "https://github.com/test/repo.git",
				Dest:    "/path/to/dest",
				Version: "main",
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"git -C /path/to/dest rev-parse HEAD": {
					err: fmt.Errorf("not a git repository"),
				},
				"git clone https://github.com/test/repo.git /path/to/dest": {
					stdout: "",
					stderr: "",
				},
				"git -C /path/to/dest checkout main": {
					stdout: "",
					stderr: "",
				},
			},
			wantOutput: GitOutput{
				RevBefore: "",
				RevAfter:  "abc123",
				Dest:      "/path/to/dest",
			},
		},
		{
			name: "update existing repo",
			input: GitInput{
				Repo:    "https://github.com/test/repo.git",
				Dest:    "/path/to/dest",
				Version: "feature-branch",
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"git -C /path/to/dest rev-parse HEAD": {
					stdout: "abc123",
				},
				"git -C /path/to/dest checkout feature-branch": {
					stdout: "",
					stderr: "",
				},
			},
			wantOutput: GitOutput{
				RevBefore: "abc123",
				RevAfter:  "def456",
				Dest:      "/path/to/dest",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := newMockHostContext()

			module := GitModule{}
			output, err := module.Execute(tt.input, mockCtx, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			gitOutput := output.(GitOutput)
			assert.Equal(t, tt.wantOutput.RevBefore, gitOutput.RevBefore)
			assert.Equal(t, tt.wantOutput.RevAfter, gitOutput.RevAfter)
			assert.Equal(t, tt.wantOutput.Dest, gitOutput.Dest)
		})
	}
}

func TestGitModule_Revert(t *testing.T) {
	tests := []struct {
		name       string
		input      GitInput
		previous   GitOutput
		mockOutput map[string]struct {
			stdout string
			stderr string
			err    error
		}
		wantOutput GitOutput
		wantErr    bool
	}{
		{
			name: "successful revert",
			input: GitInput{
				Repo: "https://github.com/test/repo.git",
				Dest: "/path/to/dest",
			},
			previous: GitOutput{
				RevBefore: "abc123",
				RevAfter:  "def456",
				Dest:      "/path/to/dest",
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"git -C /path/to/dest checkout abc123": {
					stdout: "",
					stderr: "",
				},
			},
			wantOutput: GitOutput{
				RevBefore: "def456",
				RevAfter:  "abc123",
				Dest:      "/path/to/dest",
			},
		},
		{
			name: "no previous state",
			input: GitInput{
				Repo: "https://github.com/test/repo.git",
				Dest: "/path/to/dest",
			},
			previous: GitOutput{},
			wantOutput: GitOutput{
				Dest: "/path/to/dest",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := newMockHostContext()

			module := GitModule{}
			output, err := module.Revert(tt.input, mockCtx, tt.previous, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			gitOutput := output.(GitOutput)
			assert.Equal(t, tt.wantOutput.RevBefore, gitOutput.RevBefore)
			assert.Equal(t, tt.wantOutput.RevAfter, gitOutput.RevAfter)
			assert.Equal(t, tt.wantOutput.Dest, gitOutput.Dest)
		})
	}
}

func TestGitModule_ValidateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   GitInput
		wantErr bool
	}{
		{
			name: "valid input with all fields",
			input: GitInput{
				Repo:    "https://github.com/test/repo.git",
				Dest:    "/path/to/dest",
				Version: "main",
			},
			wantErr: false,
		},
		{
			name: "valid input without version",
			input: GitInput{
				Repo: "https://github.com/test/repo.git",
				Dest: "/path/to/dest",
			},
			wantErr: false,
		},
		{
			name: "missing repo",
			input: GitInput{
				Dest: "/path/to/dest",
			},
			wantErr: true,
		},
		{
			name: "missing dest",
			input: GitInput{
				Repo: "https://github.com/test/repo.git",
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
