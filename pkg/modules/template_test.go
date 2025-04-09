package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

var (
	fixturesDir = filepath.Join(filepath.Dir(os.Args[0]), "fixtures")
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
				Src:  filepath.Join(fixturesDir, "templates", "test.conf.j2"),
				Dest: "/tmp/test.conf",
			},
			mockOutput: map[string]struct {
				fileContents string
				err          error
			}{
				filepath.Join(fixturesDir, "templates", "test.conf.j2"): {
					fileContents: "Hello {{ name }}!\n",
				},
				"/tmp/test.conf": {
					fileContents: "",
				},
			},
			wantOutput: TemplateOutput{
				Contents: pkg.RevertableChange[string]{
					Before: "",
					After:  "Hello world!\n",
				},
			},
		},
		{
			name: "update existing file",
			input: TemplateInput{
				Src:  filepath.Join(fixturesDir, "templates", "test.conf.j2"),
				Dest: "/tmp/test.conf",
			},
			mockOutput: map[string]struct {
				fileContents string
				err          error
			}{
				filepath.Join(fixturesDir, "templates", "test.conf.j2"): {
					fileContents: "Hello {{ name }}!",
				},
				"/tmp/test.conf": {
					fileContents: "Hello old world!",
				},
			},
			wantOutput: TemplateOutput{
				Contents: pkg.RevertableChange[string]{
					Before: "Hello old world!",
					After:  "Hello world!\n",
				},
			},
		},
		{
			name: "template source file not found",
			input: TemplateInput{
				Src:  filepath.Join(fixturesDir, "templates", "nonexistent.conf.j2"),
				Dest: "/tmp/test.conf",
			},
			mockOutput: map[string]struct {
				fileContents string
				err          error
			}{
				filepath.Join(fixturesDir, "templates", "nonexistent.conf.j2"): {
					err: fmt.Errorf("file not found"),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid template syntax",
			input: TemplateInput{
				Src:  filepath.Join(fixturesDir, "templates", "invalid.conf.j2"),
				Dest: "/tmp/test.conf",
			},
			mockOutput: map[string]struct {
				fileContents string
				err          error
			}{
				filepath.Join(fixturesDir, "templates", "invalid.conf.j2"): {
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
			assert.Equal(t, tt.wantOutput.Contents.Before, templateOutput.Contents.Before)
			assert.Equal(t, tt.wantOutput.Contents.After, templateOutput.Contents.After)
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
				Src:  filepath.Join(fixturesDir, "templates", "test.conf.j2"),
				Dest: "/tmp/test.conf",
			},
			previous: TemplateOutput{
				Contents: pkg.RevertableChange[string]{
					Before: "Hello old world!",
					After:  "Hello world!",
				},
			},
			mockOutput: map[string]struct {
				fileContents string
				err          error
			}{
				"/tmp/test.conf": {
					fileContents: "Hello world!",
				},
			},
			wantOutput: TemplateOutput{
				Contents: pkg.RevertableChange[string]{
					Before: "Hello world!",
					After:  "Hello old world!",
				},
			},
		},
		{
			name: "revert with write error",
			input: TemplateInput{
				Src:  filepath.Join(fixturesDir, "templates", "test.conf.j2"),
				Dest: "/tmp/test.conf",
			},
			previous: TemplateOutput{
				Contents: pkg.RevertableChange[string]{
					Before: "Hello old world!",
					After:  "Hello world!",
				},
			},
			mockOutput: map[string]struct {
				fileContents string
				err          error
			}{
				"/tmp/test.conf": {
					err: fmt.Errorf("permission denied"),
				},
			},
			wantErr: true,
		},
		{
			name: "no previous state",
			input: TemplateInput{
				Src:  filepath.Join(fixturesDir, "templates", "test.conf.j2"),
				Dest: "/tmp/test.conf",
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
			assert.Equal(t, tt.wantOutput.Contents.Before, templateOutput.Contents.Before)
			assert.Equal(t, tt.wantOutput.Contents.After, templateOutput.Contents.After)
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
				Src:  filepath.Join(fixturesDir, "templates", "test.conf.j2"),
				Dest: "/tmp/test.conf",
			},
			wantErr: false,
		},
		{
			name: "missing src",
			input: TemplateInput{
				Dest: "/tmp/test.conf",
			},
			wantErr: true,
		},
		{
			name: "missing dest",
			input: TemplateInput{
				Src: filepath.Join(fixturesDir, "templates", "test.conf.j2"),
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
