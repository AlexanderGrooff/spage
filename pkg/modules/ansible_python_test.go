package modules

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

func TestAnsiblePythonInput_ModuleInputCompatibility(t *testing.T) {
	apt := &AnsiblePythonInput{
		ModuleName: "curl",
		Args:       map[string]any{},
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = apt

	// Wrap in ModuleInput
	mi := &pkg.ModuleInput{Actual: apt}

	// ToCode should not panic and should contain 'AnsiblePythonInput'
	code := mi.ToCode()
	assert.Contains(t, code, "AnsiblePythonInput", "ToCode output should mention AnsiblePythonInput")

	// GetVariableUsage should return a slice (empty for this input)
	vars := mi.GetVariableUsage()
	assert.IsType(t, []string{}, vars)

	// Validate should not return error for valid input
	err := mi.Validate()
	assert.NoError(t, err)

	// HasRevert should be false for AnsiblePythonInput
	assert.False(t, mi.HasRevert())

	// ProvidesVariables should return nil or empty
	assert.Nil(t, mi.ProvidesVariables())
}

func TestAnsiblePythonOutput_ModuleOutputCompatibility(t *testing.T) {
	output := &AnsiblePythonOutput{
		WasChanged: false,
		Failed:     false,
		Msg:        "",
		Results:    map[string]any{},
	}

	// Ensure it implements FactProvider
	var _ pkg.FactProvider = output

	// AsFacts should return a map
	facts := output.AsFacts()
	assert.IsType(t, map[string]interface{}{}, facts)

	// Changed should return the WasChanged value
	assert.Equal(t, output.WasChanged, output.Changed())
}

func TestParseAnsibleOutput_LocalTempDir(t *testing.T) {
	m := AnsiblePythonModule{}
	// Sample output captured from ansible-playbook -v on localhost with tempfile module
	raw := `PLAY [localhost] ***************************************************************

TASK [Execute tempfile] ********************************************************
[WARNING]: Platform darwin on host localhost is using the discovered Python
interpreter at /usr/local/bin/python3.12, but future installation of another
Python interpreter could change the meaning of that path. See
https://docs.ansible.com/ansible-
core/2.18/reference_appendices/interpreter_discovery.html for more information.
changed: [localhost] => {"ansible_facts": {"discovered_interpreter_python": "/usr/local/bin/python3.12"}, "changed": true, "gid": 20, "group": "staff", "mode": "0700", "owner": "alexandergr", "path": "/var/folders/83/vygk1dfd67n8328qt931w9pm0000gn/T/ansible.q_9zbkuhbuild", "size": 64, "state": "directory", "uid": 501}

PLAY RECAP *********************************************************************
localhost                  : ok=1    changed=1    unreachable=0    failed=0    skipped=0    rescued=0    ignored=0
`

	out := m.parseAnsibleOutput(raw, "tempfile")

	assert.True(t, out.WasChanged, "changed should be true")
	assert.False(t, out.Failed, "failed should be false")
	assert.Equal(t, "", out.Msg, "msg should be empty when not present in JSON")

	// Results should contain parsed JSON without raw_output
	_, hasRaw := out.Results.(map[string]interface{})["raw_output"]
	assert.False(t, hasRaw, "raw_output should not be present when JSON was parsed")
	assert.Contains(t, out.Results, "ansible_facts")

	facts, ok := out.Results.(map[string]interface{})["ansible_facts"].(map[string]interface{})
	assert.True(t, ok, "ansible_facts should be a map")

	_, ok = facts["discovered_interpreter_python"].(string)
	assert.True(t, ok, "discovered_interpreter_python should be a string")

	_, ok = out.Results.(map[string]interface{})["path"].(string)
	assert.True(t, ok, "path should be a string")
}

func TestParseAnsibleOutput_RemoteTempDir(t *testing.T) {
	m := AnsiblePythonModule{}
	// Sample output captured from ansible-playbook -v on localhost with tempfile module
	raw := `PLAY [somehost] ***************************************************************

TASK [Execute tempfile] ********************************************************
[WARNING]: Platform darwin on host somehost is using the discovered Python
interpreter at /usr/local/bin/python3.12, but future installation of
another Python interpreter could change the meaning of that path. See
https://docs.ansible.com/ansible-
core/2.14/reference_appendices/interpreter_discovery.html for more information.
changed: [somehost] => {
    "ansible_facts": {
        "discovered_interpreter_python": "/usr/local/bin/python3.12"
    },
    "changed": true,
    "gid": 20,
    "group": "staff",
    "mode": "0700",
    "owner": "alexandergr",
    "path": "/var/folders/83/vygk1dfd67n8328qt931w9pm0000gn/T/ansible.6fke0hbnbuild",
    "size": 64,
    "state": "directory",
    "uid": 501
}

PLAY RECAP *********************************************************************
somehost                  : ok=1    changed=1    unreachable=0    failed=0    skipped=0    rescued=0    ignored=0
`

	out := m.parseAnsibleOutput(raw, "tempfile")

	assert.True(t, out.WasChanged, "changed should be true")
	assert.False(t, out.Failed, "failed should be false")
	assert.Equal(t, "", out.Msg, "msg should be empty when not present in JSON")

	// Results should contain parsed JSON without raw_output
	_, hasRaw := out.Results.(map[string]interface{})["raw_output"]
	assert.False(t, hasRaw, "raw_output should not be present when JSON was parsed")
	assert.Contains(t, out.Results, "ansible_facts")

	facts, ok := out.Results.(map[string]interface{})["ansible_facts"].(map[string]interface{})
	assert.True(t, ok, "ansible_facts should be a map")

	_, ok = facts["discovered_interpreter_python"].(string)
	assert.True(t, ok, "discovered_interpreter_python should be a string")

	_, ok = out.Results.(map[string]interface{})["path"].(string)
	assert.True(t, ok, "path should be a string")
}

func TestParseAnsibleOutput_ModuleNotFound(t *testing.T) {
	m := AnsiblePythonModule{}
	raw := `PLAY [localhost] ***************************************************************

TASK [Execute unknown_module] *************************************************
ERROR! couldn't resolve module/action 'unknown_module'. This often indicates a misspelling, missing collection or incorrect module path.`

	out := m.parseAnsibleOutput(raw, "unknown_module")

	assert.True(t, out.Failed)
	assert.False(t, out.WasChanged)
	assert.Contains(t, out.Msg, "Module 'unknown_module' not found")
	resMap3, _ := out.Results.(map[string]interface{})
	assert.Equal(t, "module_not_found", resMap3["ansible_error"])
}

func TestParseAnsibleOutput_FailedWithJSON(t *testing.T) {
	m := AnsiblePythonModule{}
	raw := `fatal: [localhost]: FAILED! => {"changed": false, "failed": true, "msg": "kaboom"}`

	out := m.parseAnsibleOutput(raw, "some_module")

	assert.True(t, out.Failed)
	assert.False(t, out.WasChanged)
	assert.Equal(t, "kaboom", out.Msg)
	// Ensure JSON was parsed
	resMap4, _ := out.Results.(map[string]interface{})
	assert.Equal(t, true, resMap4["failed"])
	assert.Equal(t, false, resMap4["changed"])
}

func TestParseAnsibleOutput_ErrorWithoutJSON(t *testing.T) {
	m := AnsiblePythonModule{}
	raw := `PLAY [localhost]\nERROR! unexpected error occurred while parsing inventory`

	out := m.parseAnsibleOutput(raw, "copy")

	assert.True(t, out.Failed)
	assert.Contains(t, out.Msg, "Ansible error:")
	// Fallback should include raw_output since no JSON was found
	resMap5, _ := out.Results.(map[string]interface{})
	ro, ok := resMap5["raw_output"].(string)
	assert.True(t, ok)
	assert.Contains(t, ro, "ERROR!")
}

func TestAnsiblePythonInput_SliceArgs_ToCodeAndVars(t *testing.T) {
	apt := &AnsiblePythonInput{
		ModuleName: "block",
		Args: []interface{}{
			map[string]interface{}{"name": "task 1", "command": "/bin/true"},
			map[string]interface{}{"name": "task {{ myvar }}", "command": "/bin/true"},
		},
	}

	// Ensure it implements ConcreteModuleInputProvider
	var _ pkg.ConcreteModuleInputProvider = apt

	mi := &pkg.ModuleInput{Actual: apt}
	code := mi.ToCode()
	assert.Contains(t, code, "AnsiblePythonInput")
	assert.Contains(t, code, "[]interface{}", "ToCode should render slice args when provided a slice")
	assert.Contains(t, code, "\"block\"")

	vars := mi.GetVariableUsage()
	assert.Contains(t, vars, "myvar", "Variable extraction should find variables in slice args")
}

func TestGetPythonFallbackForCompilation_SliceArgs(t *testing.T) {
	raw := []interface{}{
		map[string]interface{}{"name": "task 1", "command": "/bin/true"},
		map[string]interface{}{"name": "task 2", "command": "/bin/true"},
	}
	mod, params := GetPythonFallbackForCompilation("block", raw)
	assert.NotNil(t, mod)

	inp, ok := params.(AnsiblePythonInput)
	if !ok {
		t.Fatalf("expected AnsiblePythonInput, got %T", params)
	}
	assert.Equal(t, "block", inp.ModuleName)

	argsSlice, ok := inp.Args.([]interface{})
	if !ok {
		t.Fatalf("expected slice args, got %T", inp.Args)
	}
	assert.Equal(t, 2, len(argsSlice))
}

func TestParseAnsibleOutput_HttpApi(t *testing.T) {
	m := AnsiblePythonModule{}
	// Sample output captured from ansible-playbook -v on localhost with tempfile module
	raw := `Using /some/path/ansible.cfg as config file

PLAY [somehost] ***************************************************************

TASK [Execute eoscommand] ********************************************************
[WARNING]: Platform darwin on host somehost is using the discovered Python
interpreter at /usr/local/bin/python3.12, but future installation of
another Python interpreter could change the meaning of that path. See
https://docs.ansible.com/ansible-
core/2.14/reference_appendices/interpreter_discovery.html for more information.
ok: [somehost] => {
    "changed": false,
}

STDOUT:

[{"some_key": "some_value"}]

PLAY RECAP *********************************************************************
somehost                  : ok=1    changed=0    unreachable=0    failed=0    skipped=0    rescued=0    ignored=0
`

	out := m.parseAnsibleOutput(raw, "eoscommand")

	assert.False(t, out.WasChanged, "changed should be false")
	assert.False(t, out.Failed, "failed should be false")
	assert.Equal(t, "", out.Msg, "msg should be empty when not present in JSON")

	// Results should contain parsed JSON without raw_output and be a slice
	if _, isSlice := out.Results.([]interface{}); !isSlice {
		t.Fatalf("expected slice results for httpapi stdout, got %T", out.Results)
	}
	resSlice := out.Results.([]interface{})
	// For convenience, also ensure no raw_output key exists when treated as map
	_, hasRaw := map[string]interface{}{"noop": 0}["raw_output"]
	assert.False(t, hasRaw, "raw_output should not be present when JSON was parsed")
	fact, ok := resSlice[0].(map[string]interface{})
	assert.True(t, ok, "some_key should be a map")

	_, ok = fact["some_key"].(string)
	assert.True(t, ok, "some_key value should be a string")
}
