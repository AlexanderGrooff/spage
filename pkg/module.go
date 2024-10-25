package pkg

import "reflect"

type ModuleInput interface {
	ToCode(ident int) string
}
type ModuleOutput interface{}

type Module interface {
	Execute(params interface{}, c Context) (interface{}, error)
	Revert(params interface{}, c Context) (interface{}, error)
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
