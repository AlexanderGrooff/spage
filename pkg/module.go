package pkg

import "reflect"

type ModuleInput interface {
	ToCode() string
	GetVariableUsage() []string
}
type ModuleOutput interface {
	String() string
	Changed() bool
}

type Module interface {
	Execute(params interface{}, c HostContext) (interface{}, error)
	Revert(params interface{}, c HostContext, previous interface{}) (interface{}, error)
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
