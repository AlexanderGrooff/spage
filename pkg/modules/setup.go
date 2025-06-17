package modules

import (
	"fmt"
	"os"
	"reflect"

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
	for _, fact := range input.Facts {
		if _, allowed := pkg.AllowedFacts[fact]; !allowed {
			continue // skip facts not in the allowed list
		}
		var factValue interface{}
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
		case "ssh_host_pub_keys":
			// Placeholder: implement SSH host pub key gathering if needed
			factValue = nil
		}

		// Store the fact in the host context and in our return map
		facts[fact] = factValue
		closure.HostContext.Facts.Store(fact, factValue)
	}
	return SetupOutput{Facts: facts}, nil
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
