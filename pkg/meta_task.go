package pkg

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
)

type MetaTask struct {
	*TaskParams
	Children []GraphNode `yaml:"children" json:"children"`
	Rescue   []GraphNode `yaml:"rescue,omitempty" json:"rescue,omitempty"`
	Always   []GraphNode `yaml:"always,omitempty" json:"always,omitempty"`
}

func (mt *MetaTask) Params() *TaskParams {
	return mt.TaskParams
}

func (mt *MetaTask) MarshalJSON() ([]byte, error) {
	// TODO: fix this
	return json.Marshal(struct {
		Id       int         `json:"id"`
		Name     string      `json:"name"`
		Module   string      `json:"module"`
		Children []GraphNode `json:"children"`
		Params   ModuleInput `json:"params"`
	}{
		Id:       mt.Id,
		Name:     mt.Name,
		Module:   mt.Module,
		Children: mt.Children,
		Params:   mt.TaskParams.Params,
	})
}

func (mt *MetaTask) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, mt)
}

func (mt *MetaTask) GetId() int {
	return mt.Id
}

func (mt *MetaTask) SetId(id int) {
	mt.Id = id
}

func (mt *MetaTask) String() string {
	return mt.Name
}

func (mt *MetaTask) GetName() string {
	return mt.Name
}

func (mt *MetaTask) GetIsHandler() bool {
	// TODO: implement
	return false
}

func (mt *MetaTask) GetTags() []string {
	// TODO: implement
	return []string{}
}

func (mt *MetaTask) ShouldExecute(closure *Closure) (bool, error) {
	// TODO: implement
	return true, nil
}

func (mt *MetaTask) ToCode() string {
	// Don't care about indentation because we'll format with go fmt in Graph.SaveToFile
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("&pkg.MetaTask{TaskParams: %s, Children: []pkg.GraphNode{\n", mt.TaskParams.ToCode()))
	for i, task := range mt.Children {
		if i > 0 {
			sb.WriteString("  ,\n")
		}
		sb.WriteString(task.ToCode())
	}
	sb.WriteString(",\n}")

	// Add Rescue tasks if they exist
	if len(mt.Rescue) > 0 {
		sb.WriteString(", Rescue: []pkg.GraphNode{\n")
		for i, task := range mt.Rescue {
			if i > 0 {
				sb.WriteString("  ,\n")
			}
			sb.WriteString(task.ToCode())
		}
		sb.WriteString(",\n}")
	}

	// Add Always tasks if they exist
	if len(mt.Always) > 0 {
		sb.WriteString(", Always: []pkg.GraphNode{\n")
		for i, task := range mt.Always {
			if i > 0 {
				sb.WriteString("  ,\n")
			}
			sb.WriteString(task.ToCode())
		}
		sb.WriteString(",\n}")
	}

	sb.WriteString("}")
	return sb.String()
}

func (mt *MetaTask) GetVariableUsage() ([]string, error) {
	variables := []string{}

	// Collect variables from children
	for _, task := range mt.Children {
		taskVariables, err := task.GetVariableUsage()
		if err != nil {
			return nil, err
		}
		variables = append(variables, taskVariables...)
	}

	// Collect variables from rescue tasks
	for _, task := range mt.Rescue {
		taskVariables, err := task.GetVariableUsage()
		if err != nil {
			return nil, err
		}
		variables = append(variables, taskVariables...)
	}

	// Collect variables from always tasks
	for _, task := range mt.Always {
		taskVariables, err := task.GetVariableUsage()
		if err != nil {
			return nil, err
		}
		variables = append(variables, taskVariables...)
	}

	// Remove duplicates
	uniqueVariables := make(map[string]struct{})
	for _, variable := range variables {
		uniqueVariables[variable] = struct{}{}
	}
	variables = make([]string, 0, len(uniqueVariables))
	for variable := range uniqueVariables {
		variables = append(variables, variable)
	}

	return variables, nil
}

func (mt *MetaTask) ConstructClosure(c *HostContext, cfg *config.Config) *Closure {
	// TODO: implement
	return &Closure{
		HostContext: c,
		Config:      cfg,
	}
}

func (mt *MetaTask) ExecuteModule(closure *Closure) chan TaskResult {
	common.LogDebug("Executing meta task", map[string]interface{}{"task_name": mt.Name, "closure": closure.HostContext.Host.Name, "module": mt.Module})
	// TODO: should the meta task have its own task result (with duration, status etc)?
	module, ok := GetMetaModule(mt.Module)
	if !ok {
		// Try to use the Python fallback module for unknown modules
		common.LogError("Module not found in Spage", map[string]interface{}{"task_name": mt.Name, "closure": closure.HostContext.Host.Name, "module": mt.Module})
		return make(chan TaskResult)
	}
	res := module.EvaluateExecute(mt, closure)
	return res
}

func (mt *MetaTask) RevertModule(closure *Closure) chan TaskResult {
	common.LogDebug("Reverting meta task", map[string]interface{}{"task_name": mt.Name, "closure": closure.HostContext.Host.Name, "module": mt.Module})
	module, ok := GetMetaModule(mt.Module)
	if !ok {
		common.LogError("Module not found in Spage", map[string]interface{}{"task_name": mt.Name, "closure": closure.HostContext.Host.Name, "module": mt.Module})
		return make(chan TaskResult)
	}
	return module.EvaluateRevert(mt, closure)
}
