package modules

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
)

type SetupModule struct{}

type SetupInput struct {
	Facts []string `yaml:"facts"`
}

type SetupOutput struct {
	Facts map[string]interface{}
	pkg.ModuleOutput
}

func (sm SetupModule) InputType() reflect.Type {
	return reflect.TypeOf(SetupInput{})
}

func (sm SetupModule) OutputType() reflect.Type {
	return reflect.TypeOf(SetupOutput{})
}

func (i SetupInput) ToCode() string {
	return fmt.Sprintf("modules.SetupInput{Facts: %#v}", i.Facts)
}

func (i SetupInput) GetVariableUsage() []string {
	// Setup module does not use variables, it provides them.
	return nil
}

func (i SetupInput) Validate() error {
	if len(i.Facts) == 0 {
		return fmt.Errorf("no facts specified for setup module")
	}
	return nil
}

func (i SetupInput) HasRevert() bool {
	return false
}

func (i SetupInput) ProvidesVariables() []string {
	return i.Facts
}

func (o SetupOutput) String() string {
	return fmt.Sprintf("Gathered facts: %v", o.Facts)
}

func (o SetupOutput) Changed() bool {
	return false
}

func (m SetupModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	input, ok := params.(SetupInput)
	if !ok {
		return nil, fmt.Errorf("expected SetupInput, got %T", params)
	}
	facts := make(map[string]interface{})

	// Precompute distribution facts once if needed
	needsDistro := false
	for _, f := range input.Facts {
		if f == "ansible_distribution" || f == "ansible_distribution_major_version" {
			needsDistro = true
			break
		}
	}
	var distroName string
	var distroMajor string
	if needsDistro {
		distroName, distroMajor = m.getDistributionInfo(closure)
	}

	for _, fact := range input.Facts {
		if _, allowed := pkg.AllowedFacts[fact]; !allowed {
			continue // skip facts not in the allowed list
		}
		var factValue interface{}

		// This list is also maintained in pkg/graph.go named AllowedFacts
		switch fact {
		case "platform":
			factValue = closure.HostContext.Host.Host // Host.Host is the address or platform string
		case "user":
			if closure.HostContext.Host.IsLocal {
				factValue = os.Getenv("USER")
			} else {
				factValue = closure.HostContext.Host.Name // fallback, or could parse from Host struct if available
			}
		case "inventory_hostname":
			factValue = closure.HostContext.Host.Name
		case "inventory_hostname_short":
			fullName := closure.HostContext.Host.Name
			short := fullName
			if idx := strings.Index(fullName, "."); idx != -1 {
				short = fullName[:idx]
			}
			factValue = short
		case "ssh_host_pub_keys":
			// Placeholder: implement SSH host pub key gathering if needed
			factValue = nil
		case "ansible_distribution":
			factValue = distroName
		case "ansible_distribution_major_version":
			factValue = distroMajor
		}

		// Store the fact in the host context and in our return map
		facts[fact] = factValue
		closure.HostContext.Facts.Store(fact, factValue)
	}

	// After gathering facts, re-process any variables in the host context that contain Jinja templates
	// This ensures that variables like "{{ inventory_hostname }}" get resolved with the newly available facts
	err := m.reprocessVariablesWithNewFacts(closure)
	if err != nil {
		return nil, fmt.Errorf("failed to reprocess variables after gathering facts: %w", err)
	}

	return SetupOutput{Facts: facts}, nil
}

// reprocessVariablesWithNewFacts iterates through all variables in the host context
// and re-templates any string values that contain Jinja template syntax
func (m SetupModule) reprocessVariablesWithNewFacts(closure *pkg.Closure) error {
	// Collect all variables that need to be re-processed
	var variablesToUpdate []struct {
		key   string
		value string
	}

	// Iterate through all facts in the host context
	closure.HostContext.Facts.Range(func(k, v interface{}) bool {
		key, keyOk := k.(string)
		if !keyOk {
			return true // continue iteration
		}

		// Check if the value is a string that contains Jinja template syntax
		if strValue, ok := v.(string); ok {
			variablesToUpdate = append(variablesToUpdate, struct {
				key   string
				value string
			}{key, strValue})
		}
		return true // continue iteration
	})

	// Re-process the collected variables
	for _, variable := range variablesToUpdate {
		templatedValue, err := pkg.TemplateString(variable.value, closure)
		if err != nil {
			return fmt.Errorf("failed to template variable %q with value %q: %w", variable.key, variable.value, err)
		}

		// Only update if the value actually changed (to avoid unnecessary updates)
		if templatedValue != variable.value {
			closure.HostContext.Facts.Store(variable.key, templatedValue)
		}
	}

	return nil
}

func (m SetupModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	return nil, nil // No revert for setup
}

// ParameterAliases defines aliases for module parameters.
func (m SetupModule) ParameterAliases() map[string]string {
	return nil
}

func init() {
	pkg.RegisterModule("setup", SetupModule{})
}

// getDistributionInfo returns distribution name and major version for local hosts.
// It parses /etc/os-release.
func (m SetupModule) getDistributionInfo(closure *pkg.Closure) (string, string) {
	if !closure.HostContext.Host.IsLocal {
		return "", ""
	}
	// TODO: username?
	data, err := closure.HostContext.ReadFile("/etc/os-release", "")
	if err != nil {
		return "", ""
	}
	var name string
	var versionID string
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "NAME=") {
			name = trimOsReleaseValue(line[len("NAME="):])
		} else if strings.HasPrefix(line, "VERSION_ID=") {
			versionID = trimOsReleaseValue(line[len("VERSION_ID="):])
		}
	}
	major := versionID
	if idx := strings.Index(major, "."); idx != -1 {
		major = major[:idx]
	}
	return name, major
}

func trimOsReleaseValue(v string) string {
	if v == "" {
		return v
	}
	if v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	if v[0] == '\'' && v[len(v)-1] == '\'' {
		return v[1 : len(v)-1]
	}
	return v
}
