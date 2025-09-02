package pkg

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/config"
)

type TaskCollection struct {
	Id    int         `yaml:"id" json:"id"`
	Name  string      `yaml:"name" json:"name"`
	Tasks []GraphNode `yaml:"tasks" json:"tasks"`
}

func (t *TaskCollection) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Id    int         `json:"id"`
		Name  string      `json:"name"`
		Tasks []GraphNode `json:"tasks"`
	}{
		Id:    t.Id,
		Name:  t.Name,
		Tasks: t.Tasks,
	})
}

func (t *TaskCollection) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, t)
}

func (t *TaskCollection) GetId() int {
	return t.Id
}

func (t *TaskCollection) SetId(id int) {
	t.Id = id
}

func (t *TaskCollection) String() string {
	return t.Name
}

func (t *TaskCollection) GetName() string {
	return t.Name
}

func (t *TaskCollection) GetIsHandler() bool {
	// TODO: implement
	return false
}

func (t *TaskCollection) GetTags() []string {
	// TODO: implement
	return []string{}
}

func (t *TaskCollection) ToCode() string {
	// TODO: indentation
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("pkg.TaskCollection{Id: %d, Name: %s, Tasks: []pkg.GraphNode{", t.Id, t.Name))
	for _, task := range t.Tasks {
		sb.WriteString(task.ToCode())
	}
	sb.WriteString("}}")
	return sb.String()
}

func (tc *TaskCollection) GetVariableUsage() ([]string, error) {
	variables := []string{}
	for _, task := range tc.Tasks {
		taskVariables, err := task.GetVariableUsage()
		if err != nil {
			return nil, err
		}
		variables = append(variables, taskVariables...)
		uniqueVariables := make(map[string]struct{})
		for _, variable := range variables {
			uniqueVariables[variable] = struct{}{}
		}
		variables = make([]string, 0, len(uniqueVariables))
		for variable := range uniqueVariables {
			variables = append(variables, variable)
		}
	}
	return variables, nil
}

func (tc *TaskCollection) ConstructClosure(c *HostContext, cfg *config.Config) *Closure {
	// TODO: implement
	return &Closure{
		HostContext: c,
		Config:      cfg,
	}
}

func (tc *TaskCollection) ExecuteModule(closure *Closure) TaskResult {
	// TODO: implement
	return TaskResult{}
}

func (tc *TaskCollection) RevertModule(closure *Closure) TaskResult {
	// TODO: implement
	return TaskResult{}
}
