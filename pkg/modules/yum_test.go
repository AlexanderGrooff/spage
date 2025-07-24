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
				Enablerepo:  []interface{}{"epel"},
				Disablerepo: []interface{}{"updates"},
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

func TestYumInput_CommaDelimitedRepos(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		expectedEnable  []string
		expectedDisable []string
		wantErr         bool
	}{
		{
			name: "comma-delimited enablerepo",
			yaml: `
name: test-package
enablerepo: "epel, extras, updates"
`,
			expectedEnable:  []string{"epel", "extras", "updates"},
			expectedDisable: []string{},
			wantErr:         false,
		},
		{
			name: "comma-delimited disablerepo",
			yaml: `
name: test-package
disablerepo: "base, extras, updates"
`,
			expectedEnable:  []string{},
			expectedDisable: []string{"base", "extras", "updates"},
			wantErr:         false,
		},
		{
			name: "both comma-delimited repos",
			yaml: `
name: test-package
enablerepo: "epel, extras"
disablerepo: "base, updates"
`,
			expectedEnable:  []string{"epel", "extras"},
			expectedDisable: []string{"base", "updates"},
			wantErr:         false,
		},
		{
			name: "single repo string",
			yaml: `
name: test-package
enablerepo: "epel"
`,
			expectedEnable:  []string{"epel"},
			expectedDisable: []string{},
			wantErr:         false,
		},
		{
			name: "empty string",
			yaml: `
name: test-package
enablerepo: ""
`,
			expectedEnable:  []string{},
			expectedDisable: []string{},
			wantErr:         false,
		},
		{
			name: "whitespace in comma-delimited",
			yaml: `
name: test-package
enablerepo: "epel , extras , updates"
`,
			expectedEnable:  []string{"epel", "extras", "updates"},
			expectedDisable: []string{},
			wantErr:         false,
		},
		{
			name: "mixed format repositories",
			yaml: `
name: test-package
enablerepo: ["epel", "extras"]
disablerepo: "base, updates"
`,
			expectedEnable:  []string{"epel", "extras"},
			expectedDisable: []string{"base", "updates"},
			wantErr:         false,
		},
		{
			name: "list format repositories",
			yaml: `
name: test-package
enablerepo: ["epel", "extras"]
disablerepo: ["base", "updates"]
`,
			expectedEnable:  []string{"epel", "extras"},
			expectedDisable: []string{"base", "updates"},
			wantErr:         false,
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
				return
			}

			assert.NoError(t, err)

			// Validate to trigger repository parsing
			err = got.Validate()
			assert.NoError(t, err)

			// Check enablerepo parsing
			assert.Equal(t, tt.expectedEnable, got.EnablerepoList, "EnablerepoList mismatch")

			// Check disablerepo parsing
			assert.Equal(t, tt.expectedDisable, got.DisablerepoList, "DisablerepoList mismatch")
		})
	}
}

func TestYumInput_CommaDelimitedReposValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   *YumInput
		wantErr bool
	}{
		{
			name: "valid comma-delimited enablerepo",
			input: &YumInput{
				Name:       "test-package",
				Enablerepo: "epel, extras, updates",
			},
			wantErr: false,
		},
		{
			name: "valid comma-delimited disablerepo",
			input: &YumInput{
				Name:        "test-package",
				Disablerepo: "base, extras",
			},
			wantErr: false,
		},
		{
			name: "valid mixed format",
			input: &YumInput{
				Name:        "test-package",
				Enablerepo:  []string{"epel", "extras"},
				Disablerepo: "base, updates",
			},
			wantErr: false,
		},
		{
			name: "invalid enablerepo type",
			input: &YumInput{
				Name:       "test-package",
				Enablerepo: 123, // Invalid type
			},
			wantErr: true,
		},
		{
			name: "invalid disablerepo type",
			input: &YumInput{
				Name:        "test-package",
				Disablerepo: 123, // Invalid type
			},
			wantErr: true,
		},
		{
			name: "empty enablerepo string",
			input: &YumInput{
				Name:       "test-package",
				Enablerepo: "",
			},
			wantErr: false,
		},
		{
			name: "empty disablerepo string",
			input: &YumInput{
				Name:        "test-package",
				Disablerepo: "",
			},
			wantErr: false,
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

func TestYumInput_CommaDelimitedReposToCode(t *testing.T) {
	tests := []struct {
		name            string
		input           *YumInput
		expectedEnable  string
		expectedDisable string
	}{
		{
			name: "comma-delimited enablerepo",
			input: &YumInput{
				Name:       "test-package",
				Enablerepo: "epel, extras, updates",
			},
			expectedEnable:  `[]string{"epel", "extras", "updates"}`,
			expectedDisable: "nil",
		},
		{
			name: "comma-delimited disablerepo",
			input: &YumInput{
				Name:        "test-package",
				Disablerepo: "base, extras",
			},
			expectedEnable:  "nil",
			expectedDisable: `[]string{"base", "extras"}`,
		},
		{
			name: "both comma-delimited repos",
			input: &YumInput{
				Name:        "test-package",
				Enablerepo:  "epel, extras",
				Disablerepo: "base, updates",
			},
			expectedEnable:  `[]string{"epel", "extras"}`,
			expectedDisable: `[]string{"base", "updates"}`,
		},
		{
			name: "empty repos",
			input: &YumInput{
				Name: "test-package",
			},
			expectedEnable:  "nil",
			expectedDisable: "nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate to trigger parsing
			err := tt.input.Validate()
			assert.NoError(t, err)

			// Generate code
			code := tt.input.ToCode()

			// Check that the generated code contains the expected repository lists
			if tt.expectedEnable != "nil" {
				assert.Contains(t, code, tt.expectedEnable, "Generated code should contain expected enablerepo")
			} else {
				assert.Contains(t, code, "Enablerepo: nil", "Generated code should contain nil enablerepo")
			}

			if tt.expectedDisable != "nil" {
				assert.Contains(t, code, tt.expectedDisable, "Generated code should contain expected disablerepo")
			} else {
				assert.Contains(t, code, "Disablerepo: nil", "Generated code should contain nil disablerepo")
			}
		})
	}
}

func TestYumInput_ExcludeParameter(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		expectedExclude []string
		wantErr         bool
	}{
		{
			name: "comma-delimited exclude",
			yaml: `
name: test-package
exclude: "kernel, kernel-devel"
`,
			expectedExclude: []string{"kernel", "kernel-devel"},
			wantErr:         false,
		},
		{
			name: "list format exclude",
			yaml: `
name: test-package
exclude: ["kernel", "kernel-devel"]
`,
			expectedExclude: []string{"kernel", "kernel-devel"},
			wantErr:         false,
		},
		{
			name: "single exclude package",
			yaml: `
name: test-package
exclude: "kernel"
`,
			expectedExclude: []string{"kernel"},
			wantErr:         false,
		},
		{
			name: "empty exclude string",
			yaml: `
name: test-package
exclude: ""
`,
			expectedExclude: []string{},
			wantErr:         false,
		},
		{
			name: "whitespace in comma-delimited exclude",
			yaml: `
name: test-package
exclude: "kernel , kernel-devel , kernel-headers"
`,
			expectedExclude: []string{"kernel", "kernel-devel", "kernel-headers"},
			wantErr:         false,
		},
		{
			name: "mixed parameters with exclude",
			yaml: `
name: test-package
enablerepo: "epel"
exclude: "kernel, kernel-devel"
`,
			expectedExclude: []string{"kernel", "kernel-devel"},
			wantErr:         false,
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
				return
			}

			assert.NoError(t, err)

			// Validate to trigger parsing
			err = got.Validate()
			assert.NoError(t, err)

			// Check exclude parsing
			assert.Equal(t, tt.expectedExclude, got.ExcludeList, "ExcludeList mismatch")
		})
	}
}

func TestYumInput_ExcludeValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   *YumInput
		wantErr bool
	}{
		{
			name: "valid comma-delimited exclude",
			input: &YumInput{
				Name:    "test-package",
				Exclude: "kernel, kernel-devel",
			},
			wantErr: false,
		},
		{
			name: "valid list format exclude",
			input: &YumInput{
				Name:    "test-package",
				Exclude: []string{"kernel", "kernel-devel"},
			},
			wantErr: false,
		},
		{
			name: "valid mixed format",
			input: &YumInput{
				Name:       "test-package",
				Enablerepo: "epel",
				Exclude:    "kernel, kernel-devel",
			},
			wantErr: false,
		},
		{
			name: "invalid exclude type",
			input: &YumInput{
				Name:    "test-package",
				Exclude: 123, // Invalid type
			},
			wantErr: true,
		},
		{
			name: "empty exclude string",
			input: &YumInput{
				Name:    "test-package",
				Exclude: "",
			},
			wantErr: false,
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

func TestYumInput_ExcludeToCode(t *testing.T) {
	tests := []struct {
		name            string
		input           *YumInput
		expectedExclude string
	}{
		{
			name: "comma-delimited exclude",
			input: &YumInput{
				Name:    "test-package",
				Exclude: "kernel, kernel-devel",
			},
			expectedExclude: `[]string{"kernel", "kernel-devel"}`,
		},
		{
			name: "list format exclude",
			input: &YumInput{
				Name:    "test-package",
				Exclude: []string{"kernel", "kernel-devel"},
			},
			expectedExclude: `[]string{"kernel", "kernel-devel"}`,
		},
		{
			name: "empty exclude",
			input: &YumInput{
				Name: "test-package",
			},
			expectedExclude: "nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate to trigger parsing
			err := tt.input.Validate()
			assert.NoError(t, err)

			// Generate code
			code := tt.input.ToCode()

			// Check that the generated code contains the expected exclude list
			if tt.expectedExclude != "nil" {
				assert.Contains(t, code, tt.expectedExclude, "Generated code should contain expected exclude")
			} else {
				assert.Contains(t, code, "Exclude: nil", "Generated code should contain nil exclude")
			}
		})
	}
}
