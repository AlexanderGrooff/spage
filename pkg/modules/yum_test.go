package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestYumInput_ModuleInputCompatibility(t *testing.T) {
	yum := &YumInput{
		Name:        "curl",
		State:       "present",
		UpdateCache: false,
		Enablerepo:  []string{"epel"},
		Disablerepo: []string{"updates"},
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = yum

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: yum}

	// ToCode should not panic and should contain 'YumInput'
	code := mi.ToCode()
	assert.Contains(t, code, "YumInput", "ToCode output should mention YumInput")

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

func TestYumInput_Validation(t *testing.T) {
	tests := []struct {
		name    string
		input   *YumInput
		wantErr bool
	}{
		{
			name: "valid single package",
			input: &YumInput{
				Name:  "curl",
				State: "present",
			},
			wantErr: false,
		},
		{
			name: "valid package list",
			input: &YumInput{
				Name:  []string{"curl", "wget"},
				State: "present",
			},
			wantErr: false,
		},
		{
			name: "valid update_cache only",
			input: &YumInput{
				UpdateCache: true,
			},
			wantErr: false,
		},
		{
			name: "invalid state",
			input: &YumInput{
				Name:  "curl",
				State: "invalid",
			},
			wantErr: true,
		},
		{
			name: "empty name without update_cache",
			input: &YumInput{
				Name:  "",
				State: "present",
			},
			wantErr: true,
		},
		{
			name: "nil name without update_cache",
			input: &YumInput{
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

func TestYumInput_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    *YumInput
		wantErr bool
	}{
		{
			name: "shorthand string",
			yaml: "curl",
			want: &YumInput{
				Name:        "curl",
				State:       "present",
				UpdateCache: false,
				PkgNames:    []string{"curl"},
			},
			wantErr: false,
		},
		{
			name: "full map",
			yaml: `
name: curl
state: latest
update_cache: true
enablerepo: [epel]
disablerepo: [updates]
`,
			want: &YumInput{
				Name:        "curl",
				State:       "latest",
				UpdateCache: true,
				Enablerepo:  []string{"epel"},
				Disablerepo: []string{"updates"},
				PkgNames:    []string{"curl"},
			},
			wantErr: false,
		},
		{
			name: "package list",
			yaml: `
name: [curl, wget]
state: absent
`,
			want: &YumInput{
				Name:        []interface{}{"curl", "wget"},
				State:       "absent",
				UpdateCache: false,
				PkgNames:    []string{"curl", "wget"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got YumInput

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
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want.Name, got.Name)
				assert.Equal(t, tt.want.State, got.State)
				assert.Equal(t, tt.want.UpdateCache, got.UpdateCache)
				assert.Equal(t, tt.want.Enablerepo, got.Enablerepo)
				assert.Equal(t, tt.want.Disablerepo, got.Disablerepo)
				assert.Equal(t, tt.want.PkgNames, got.PkgNames)
			}
		})
	}
}

func TestYumModule_Registration(t *testing.T) {
	// Test that the yum module is properly registered
	_, ok := pkg.GetModule("yum")
	assert.True(t, ok, "yum module should be registered")

	// Test that the ansible.builtin.yum alias is also registered
	_, ok = pkg.GetModule("ansible.builtin.yum")
	assert.True(t, ok, "ansible.builtin.yum module should be registered")

	// Test that we get the correct module type
	mod, _ := pkg.GetModule("yum")
	assert.IsType(t, YumModule{}, mod, "yum module should be of type YumModule")

	mod, _ = pkg.GetModule("dnf")
	assert.IsType(t, YumModule{}, mod, "dnf module should be of type YumModule")

	mod, _ = pkg.GetModule("ansible.builtin.dnf")
	assert.IsType(t, YumModule{}, mod, "ansible.builtin.dnf module should be of type YumModule")
}
