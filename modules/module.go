package modules

type Module interface {
	Execute(params map[string]interface{}) error
	Revert(params map[string]interface{}) error
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
