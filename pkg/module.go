package pkg

import "reflect"

type ModuleInput interface {
	ToCode() string
	GetVariableUsage() []string
	Validate() error
}
type ModuleOutput interface {
	String() string
	Changed() bool
}

func OutputToFacts(output ModuleOutput) Facts {
	vars := make(Facts)
	v := reflect.ValueOf(output)
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)
		vars[field.Name] = value.Interface()
	}

	vars["Changed"] = output.Changed()
	return vars
}

type Module interface {
	Execute(params ModuleInput, c *HostContext, runAs string) (ModuleOutput, error)
	Revert(params ModuleInput, c *HostContext, previous ModuleOutput, runAs string) (ModuleOutput, error)
	InputType() reflect.Type
	OutputType() reflect.Type
}

// ModuleRegistry stores available modules
var ModuleRegistry = make(map[string]Module)

func RegisterModule(name string, module Module) {
	ModuleRegistry[name] = module
}

func GetModule(name string) (Module, bool) {
	module, ok := ModuleRegistry[name]
	return module, ok
}
