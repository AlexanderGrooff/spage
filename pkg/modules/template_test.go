package modules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

var (
	fixturesDir = filepath.Join(filepath.Dir(os.Args[0]), "fixtures")
)

func TestTemplateModule_ValidateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   TemplateInput
		wantErr bool
	}{
		{
			name: "valid input",
			input: TemplateInput{
				Src: filepath.Join(fixturesDir, "templates", "test.conf.j2"),
				Dst: "/tmp/test.conf",
			},
			wantErr: false,
		},
		{
			name: "missing src",
			input: TemplateInput{
				Dst: "/tmp/test.conf",
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

func TestTemplateInput_ModuleInputCompatibility(t *testing.T) {
	templateInput := &TemplateInput{
		Src:  "templates/config.j2",
		Dst:  "/etc/app/config.conf",
		Mode: "0644",
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = templateInput

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: templateInput}

	// ToCode should not panic and should contain 'TemplateInput'
	code := mi.ToCode()
	assert.Contains(t, code, "TemplateInput", "ToCode output should mention TemplateInput")

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
