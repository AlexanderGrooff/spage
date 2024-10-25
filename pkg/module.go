package pkg

type ModuleInput map[string]interface{}
type ModuleOutput map[string]interface{}

type Module interface {
	Execute(params ModuleInput, c Context) (ModuleOutput, error)
	Revert(params ModuleInput, c Context) (ModuleOutput, error)
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
