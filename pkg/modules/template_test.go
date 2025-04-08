package modules

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTemplateModule_Execute(t *testing.T) {
	tests := []struct {
		name       string
		input      TemplateInput
		mockOutput map[string]struct {
			fileContents string
			err          error
		}
		wantOutput TemplateOutput
		wantErr    bool
	}{
		{
			name: "template new file",
			input: TemplateInput{
				Src:  "templates/test.conf.j2",
				Dest: "/etc/test.conf",
			},
			mockOutput: map[string]struct {
				fileContents string
				err          error
			}{
				"templates/test.conf.j2": {
					fileContents: "Hello {{ name }}!",
				},
				"/etc/test.conf": {
					fileContents: "",
				},
			},
			wantOutput: TemplateOutput{
				OriginalContents: "",
				NewContents:      "Hello world!",
			},
		},
		{
			name: "update existing file",
			input: TemplateInput{
				Src:  "templates/test.conf.j2",
				Dest: "/etc/test.conf",
			},
			mockOutput: map[string]struct {
				fileContents string
				err          error
			}{
				"templates/test.conf.j2": {
					fileContents: "Hello {{ name }}!",
				},
				"/etc/test.conf": {
					fileContents: "Hello old world!",
				},
			},
			wantOutput: TemplateOutput{
				OriginalContents: "Hello old world!",
				NewContents:      "Hello world!",
			},
		},
		{
			name: "template source file not found",
			input: TemplateInput{
				Src:  "templates/nonexistent.conf.j2",
				Dest: "/etc/test.conf",
			},
			mockOutput: map[string]struct {
				fileContents string
				err          error
			}{
				"templates/nonexistent.conf.j2": {
					err: fmt.Errorf("file not found"),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid template syntax",
			input: TemplateInput{
				Src:  "templates/invalid.conf.j2",
				Dest: "/etc/test.conf",
			},
			mockOutput: map[string]struct {
				fileContents string
				err          error
			}{
				"templates/invalid.conf.j2": {
					fileContents: "Hello {{ invalid syntax",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := newMockHostContext()
			mockCtx.Facts["name"] = "world"

			module := TemplateModule{}
			output, err := module.Execute(tt.input, mockCtx, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			templateOutput := output.(TemplateOutput)
			assert.Equal(t, tt.wantOutput.OriginalContents, templateOutput.OriginalContents)
			assert.Equal(t, tt.wantOutput.NewContents, templateOutput.NewContents)
		})
	}
}

func TestTemplateModule_Revert(t *testing.T) {
	tests := []struct {
		name       string
		input      TemplateInput
		previous   TemplateOutput
		mockOutput map[string]struct {
			fileContents string
			err          error
		}
		wantOutput TemplateOutput
		wantErr    bool
	}{
		{
			name: "revert file changes",
			input: TemplateInput{
				Src:  "templates/test.conf.j2",
				Dest: "/etc/test.conf",
			},
			previous: TemplateOutput{
				OriginalContents: "Hello old world!",
				NewContents:      "Hello world!",
			},
			mockOutput: map[string]struct {
				fileContents string
				err          error
			}{
				"/etc/test.conf": {
					fileContents: "Hello world!",
				},
			},
			wantOutput: TemplateOutput{
				OriginalContents: "Hello world!",
				NewContents:      "Hello old world!",
			},
		},
		{
			name: "revert with write error",
			input: TemplateInput{
				Src:  "templates/test.conf.j2",
				Dest: "/etc/test.conf",
			},
			previous: TemplateOutput{
				OriginalContents: "Hello old world!",
				NewContents:      "Hello world!",
			},
			mockOutput: map[string]struct {
				fileContents string
				err          error
			}{
				"/etc/test.conf": {
					err: fmt.Errorf("permission denied"),
				},
			},
			wantErr: true,
		},
		{
			name: "no previous state",
			input: TemplateInput{
				Src:  "templates/test.conf.j2",
				Dest: "/etc/test.conf",
			},
			previous:   TemplateOutput{},
			wantOutput: TemplateOutput{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := newMockHostContext()

			module := TemplateModule{}
			output, err := module.Revert(tt.input, mockCtx, tt.previous, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			templateOutput := output.(TemplateOutput)
			assert.Equal(t, tt.wantOutput.OriginalContents, templateOutput.OriginalContents)
			assert.Equal(t, tt.wantOutput.NewContents, templateOutput.NewContents)
		})
	}
}

func TestTemplateModule_ValidateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   TemplateInput
		wantErr bool
	}{
		{
			name: "valid input",
			input: TemplateInput{
				Src:  "templates/test.conf.j2",
				Dest: "/etc/test.conf",
			},
			wantErr: false,
		},
		{
			name: "missing src",
			input: TemplateInput{
				Dest: "/etc/test.conf",
			},
			wantErr: true,
		},
		{
			name: "missing dest",
			input: TemplateInput{
				Src: "templates/test.conf.j2",
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
