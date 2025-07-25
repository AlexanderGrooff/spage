package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestAptInput_ModuleInputCompatibility(t *testing.T) {
	apt := &AptInput{
		Name:        "curl",
		State:       "present",
		UpdateCache: false,
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = apt

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: apt}

	// ToCode should not panic and should contain 'AptInput'
	code := mi.ToCode()
	assert.Contains(t, code, "AptInput", "ToCode output should mention AptInput")

	// GetVariableUsage should return a slice (empty for this input)
	vars := mi.GetVariableUsage()
	assert.IsType(t, []string{}, vars)

	// Validate should not return error for valid input
	err := mi.Validate()
	assert.NoError(t, err)

	// HasRevert should be true for present/absent with a name
	assert.True(t, mi.HasRevert())

	// ProvidesVariables should return nil or empty
	assert.Nil(t, mi.ProvidesVariables())
}

func TestAptInput_PackageNames(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		expectedNames []string
		wantErr       bool
	}{
		{
			name: "single package",
			yaml: `
name: test-package
`,
			expectedNames: []string{"test-package"},
			wantErr:       false,
		},
		{
			name: "list of packages",
			yaml: `
name: ["curl", "wget"]
`,
			expectedNames: []string{"curl", "wget"},
			wantErr:       false,
		},
		{
			name: "jinja string list",
			yaml: `
name: "['curl', 'wget']"
`,
			expectedNames: []string{"curl", "wget"},
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got AptInput

			// Parse YAML string into yaml.Node
			var node yaml.Node
			err := yaml.Unmarshal([]byte(tt.yaml), &node)
			if err != nil {
				t.Fatalf("Failed to parse YAML: %v", err)
			}

			// Use the first document node
			if len(node.Content) > 0 {
				err = got.UnmarshalYAML(node.Content[0])
			} else {
				err = got.UnmarshalYAML(&node)
			}

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			// Validate to trigger repository parsing
			err = got.Validate()
			assert.NoError(t, err)

			// Check enablerepo parsing
			assert.Equal(t, tt.expectedNames, got.PkgNames, "PkgNames mismatch")
		})
	}
}
