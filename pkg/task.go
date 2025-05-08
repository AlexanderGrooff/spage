package pkg

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/common"
	// Remove pongo2 import if no longer needed directly here
	// "github.com/flosch/pongo2"
	"gopkg.in/yaml.v3"
)

type TaskStatus string

const (
	TaskStatusSkipped TaskStatus = "skipped"
	TaskStatusFailed  TaskStatus = "failed"
	TaskStatusChanged TaskStatus = "changed"
	TaskStatusOk      TaskStatus = "ok"
)

func (s TaskStatus) String() string {
	return string(s)
}

// Useful for having a single type to pass around in channels
type TaskResult struct {
	Output   ModuleOutput
	Error    error // This can now be nil, a normal error, or an IgnoredTaskError
	Closure  *Closure
	Task     Task
	Duration time.Duration
	Status   TaskStatus
	Failed   bool
	Changed  bool
	// ExecutionSpecificOutput can store runner-specific results, e.g., SpageActivityResult for Temporal.
	ExecutionSpecificOutput interface{}
}

// IgnoredTaskError is a custom error type used when a task fails
// but IgnoreErrors is set to true.
// It wraps the original error.
type IgnoredTaskError struct {
	OriginalErr error
}

// Error implements the error interface for IgnoredTaskError.
func (e *IgnoredTaskError) Error() string {
	if e.OriginalErr != nil {
		return fmt.Sprintf("ignored: %s", e.OriginalErr.Error())
	}
	return "ignored error (no original error specified)"
}

// Unwrap allows errors.Is and errors.As to work with the wrapped original error.
func (e *IgnoredTaskError) Unwrap() error {
	return e.OriginalErr
}

// FactProvider is an interface that module outputs can implement
// to provide a map representation suitable for registering as facts.
type FactProvider interface {
	AsFacts() map[string]interface{}
}

type Task struct {
	Id           int         `yaml:"id" json:"id"`
	Name         string      `yaml:"name" json:"name"`
	Module       string      `yaml:"module" json:"module"`
	Params       ModuleInput `yaml:"params" json:"params"`
	Validate     string      `yaml:"validate" json:"validate,omitempty"`
	Before       string      `yaml:"before" json:"before,omitempty"`
	After        string      `yaml:"after" json:"after,omitempty"`
	When         string      `yaml:"when" json:"when,omitempty"`
	Register     string      `yaml:"register" json:"register,omitempty"`
	RunAs        string      `yaml:"run_as" json:"run_as,omitempty"`
	IgnoreErrors bool        `yaml:"ignore_errors,omitempty" json:"ignore_errors,omitempty"`
	FailedWhen   string      `yaml:"failed_when,omitempty" json:"failed_when,omitempty"`
	ChangedWhen  string      `yaml:"changed_when,omitempty" json:"changed_when,omitempty"`
	Loop         interface{} `yaml:"loop,omitempty" json:"loop,omitempty"`
}

// UnmarshalJSON implements the json.Unmarshaler interface for Task.
func (t *Task) UnmarshalJSON(data []byte) error {
	type Alias Task // Use type alias to avoid recursion during unmarshaling
	aux := &struct {
		Params json.RawMessage `json:"params"`
		*Alias
	}{
		Alias: (*Alias)(t),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return fmt.Errorf("failed to unmarshal task aux struct: %w", err)
	}

	// If Module or Params are not present, or params is null, no further processing for Params.Actual
	if t.Module == "" || len(aux.Params) == 0 || string(aux.Params) == "null" {
		t.Params.Actual = nil // Ensure Actual is nil if no params or module
		return nil
	}

	mod, ok := GetModule(t.Module)
	if !ok {
		return fmt.Errorf("module %s not found during task JSON unmarshaling", t.Module)
	}

	inputType := mod.InputType()
	// Create a new instance of the specific module input type (e.g., *ShellInput)
	// inputVal will be a pointer to the zero value of the input type.
	inputValPtr := reflect.New(inputType)

	// Unmarshal the raw JSON params into this specific input type instance (pointer)
	if err := json.Unmarshal(aux.Params, inputValPtr.Interface()); err != nil {
		return fmt.Errorf("failed to unmarshal params for module %s: %w", t.Module, err)
	}

	// inputValPtr is the pointer (e.g., *ShellInput). Get the value it points to.
	elemValue := inputValPtr.Elem()

	// Check if the VALUE type implements the interface.
	actualParamProvider, ok := elemValue.Interface().(ConcreteModuleInputProvider)
	if ok {
		// If the value type implements it, store the value.
		t.Params.Actual = actualParamProvider
	} else {
		// Check if the POINTER type implements the interface (less common case for modules).
		actualParamProvider, ok = inputValPtr.Interface().(ConcreteModuleInputProvider)
		if ok {
			// If the pointer type implements it, store the pointer.
			t.Params.Actual = actualParamProvider
		} else {
			// Neither value nor pointer implements the interface - should not happen for registered modules.
			return fmt.Errorf("module %s params type %T (or its pointer) does not implement ConcreteModuleInputProvider", t.Module, elemValue.Interface())
		}
	}

	return nil
}

// MarshalJSON implements the json.Marshaler interface for Task.
// This ensures that the Params field is marshaled correctly by handling the Actual field.
func (t Task) MarshalJSON() ([]byte, error) {
	// Use a type alias to avoid recursion when marshaling other fields.
	type Alias Task
	// Create an auxiliary struct to handle standard fields and the special Params field.
	aux := &struct {
		*Alias
		Params json.RawMessage `json:"params,omitempty"` // Use RawMessage to hold pre-marshaled params
	}{
		Alias: (*Alias)(&t),
	}

	// Marshal the actual parameters stored in t.Params.Actual.
	var paramsBytes []byte
	var err error
	if t.Params.Actual != nil {
		paramsBytes, err = json.Marshal(t.Params.Actual)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal Task.Params.Actual for module %s: %w", t.Module, err)
		}
	} else {
		// If Actual is nil, represent params as JSON null or an empty object.
		// An empty object {} might be safer for downstream compatibility.
		paramsBytes = []byte("{}")
	}
	aux.Params = paramsBytes // Assign the marshaled bytes

	// Marshal the auxiliary struct which now has correctly marshaled params.
	return json.Marshal(aux)
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for Task.
func (t *Task) UnmarshalYAML(node *yaml.Node) error {
	type Alias Task // Use type alias to avoid recursion
	// Create an auxiliary struct to capture all fields except Params initially,
	// and capture Params as a raw yaml.Node.
	auxTask := &struct {
		*Alias
		ParamsNode yaml.Node `yaml:"params"` // Capture 'params' field as a yaml.Node
	}{
		Alias: (*Alias)(t),
	}

	if err := node.Decode(&auxTask); err != nil {
		// This might be a partial decode if 'params' is complex and not decoded into auxTask.ParamsNode correctly by default.
		// Let's try decoding into Alias first, then extract params node if that fails or params is empty.
	}

	// Attempt to unmarshal everything into the Alias first
	if err := node.Decode((*Alias)(t)); err != nil {
		return fmt.Errorf("failed to unmarshal task alias: %w", err)
	}

	// Now, specifically extract the params node for custom processing.
	// We need to find the 'params' key in the YAML mapping node.
	var paramsSubNode *yaml.Node
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value == "params" {
				paramsSubNode = node.Content[i+1]
				break
			}
		}
	}

	if t.Module == "" || paramsSubNode == nil || paramsSubNode.Tag == "!!null" {
		t.Params.Actual = nil // No module or no params, so Actual is nil
		return nil
	}

	mod, ok := GetModule(t.Module)
	if !ok {
		return fmt.Errorf("module %s not found during task YAML unmarshaling", t.Module)
	}

	inputType := mod.InputType() // e.g., reflect.TypeOf(ShellInput{})
	// Create a new instance of the specific module input type (e.g., a pointer to ShellInput)
	inputVal := reflect.New(inputType).Interface() // This is *ShellInput

	// Decode the paramsSubNode into the specific input type instance.
	// This allows types like ShellInput to use their own UnmarshalYAML if they have one.
	if err := paramsSubNode.Decode(inputVal); err != nil {
		return fmt.Errorf("failed to decode params for module %s from YAML node: %w", t.Module, err)
	}

	// inputVal is now a pointer to the populated struct (e.g., *ShellInput).
	// We need to ensure it implements ConcreteModuleInputProvider.
	actualParamProvider, ok := inputVal.(ConcreteModuleInputProvider)
	if !ok {
		// If the pointer type doesn't implement, check if the value type does.
		// This can happen if methods are defined on T, not *T.
		// However, for unmarshaling into, we usually pass a pointer.
		// And methods like ToCode might be on the value type for ShellInput.
		// Let's assume methods are on value type as per ShellInput.ToCode() etc.
		// reflect.New(inputType) gives *Type. Interface() is *Type.
		// If ConcreteModuleInputProvider is implemented by Type, then actualParamProvider is reflect.ValueOf(inputVal).Elem().Interface().(ConcreteModuleInputProvider)

		val := reflect.ValueOf(inputVal)
		if val.Kind() == reflect.Ptr {
			actualParamProvider, ok = val.Elem().Interface().(ConcreteModuleInputProvider)
		}
		if !ok {
			return fmt.Errorf("failed to assert module %s params type %T to ConcreteModuleInputProvider after YAML decode", t.Module, inputVal)
		}
	}
	t.Params.Actual = actualParamProvider
	return nil
}

func (t Task) ToCode() string {
	var sb strings.Builder
	// Get the code representation of the actual parameters
	actualParamsCode := "nil" // Default to nil if Actual is not populated
	if t.Params.Actual != nil {
		actualParamsCode = t.Params.Actual.ToCode()
	}

	// Construct the Task literal, wrapping the actual params code in pkg.ModuleInput
	sb.WriteString(fmt.Sprintf("pkg.Task{Id: %d, Name: %q, Module: %q, Register: %q, Params: pkg.ModuleInput{Actual: %s}, RunAs: %q, When: %q",
		t.Id,
		t.Name,
		t.Module,
		t.Register,
		actualParamsCode, // Use the generated code for the Actual field
		t.RunAs,
		t.When,
	))

	if t.IgnoreErrors {
		sb.WriteString(fmt.Sprintf(", IgnoreErrors: %t", t.IgnoreErrors))
	}
	if t.FailedWhen != "" {
		sb.WriteString(fmt.Sprintf(", FailedWhen: %q", t.FailedWhen))
	}
	if t.ChangedWhen != "" {
		sb.WriteString(fmt.Sprintf(", ChangedWhen: %q", t.ChangedWhen))
	}
	// Handle Loop field in ToCode: generate code for string or slice.
	switch v := t.Loop.(type) {
	case string:
		if v != "" {
			sb.WriteString(fmt.Sprintf(", Loop: %q", v))
		}
	case []interface{}:
		if len(v) > 0 {
			sb.WriteString(", Loop: []interface{}{")
			for i, item := range v {
				sb.WriteString(fmt.Sprintf("%#v", item)) // Use %#v for Go syntax representation
				if i < len(v)-1 {
					sb.WriteString(", ")
				}
			}
			sb.WriteString("}")
		}
		// Add cases for other expected list types if necessary (e.g., []string)
	}

	sb.WriteString("},\n") // Removed the trailing newline here as it's added later if needed
	return sb.String()
}

func (t Task) String() string {
	return t.Name
}

func (t Task) ShouldExecute(closure *Closure) bool {
	if t.When != "" {
		templatedWhen, err := EvaluateExpression(t.When, closure)
		if err != nil {
			// If templating fails, we cannot evaluate the condition, so skip the task
			common.LogWarn("Error templating when condition, skipping task", map[string]interface{}{
				"task":      t.Name,
				"host":      closure.HostContext.Host.Name,
				"condition": t.When,
				"error":     err.Error(),
			})
			return false
		}

		// Evaluate truthiness using the helper function
		conditionMet := IsExpressionTruthy(templatedWhen)
		trimmedResult := strings.TrimSpace(templatedWhen) // Still needed for logging

		// Log the evaluation result
		common.DebugOutput("Evaluated when condition %q -> %q: %t",
			t.When, trimmedResult, conditionMet)

		// Return the evaluated truthiness
		return conditionMet
	}
	// If no 'when' condition, always execute
	return true
}

func (t Task) ExecuteModule(closure *Closure) TaskResult {
	startTime := time.Now()
	r := TaskResult{Task: t, Closure: closure, Status: TaskStatusSkipped}

	if !t.ShouldExecute(closure) {
		common.LogDebug("Skipping execution of task", map[string]interface{}{
			"task": t.Name,
			"host": closure.HostContext.Host.Name,
		})
		return r
	}

	module, ok := GetModule(t.Module)
	if !ok {
		r.Error = fmt.Errorf("module %s not found", t.Module)
		return r
	}

	// Pass t.Params.Actual to module.Execute
	if t.Params.Actual == nil {
		// This can happen if YAML/JSON params were empty or module not found during unmarshal
		// Or if the module genuinely takes no parameters.
		// We need a way for modules to declare if they accept nil params.
		// For now, if Actual is nil, we might need to create a zero value of InputType if possible,
		// or the module must handle nil params.
		// Let's assume modules that require params will have Actual populated.
		// If a module *can* take no params, its InputType might be an empty struct or similar.
		// And its Validate() should reflect that.
		// We should ensure `Actual` is a valid (even if zero) instance of ConcreteModuleInputProvider.
		// The unmarshalers should ensure Actual is populated with a zero value if params are empty but module exists.
		// For now, let's proceed, modules should validate their params. This might panic if module expects non-nil.
	}

	common.DebugOutput("Executing module %s with params %v and context %v", t.Module, t.Params.Actual, closure)
	r.Output, r.Error = module.Execute(t.Params.Actual, closure, t.RunAs)
	duration := time.Since(startTime)
	r.Duration = duration

	return handleResult(&r, t, closure)
}

func (t Task) RevertModule(closure *Closure) TaskResult {
	startTime := time.Now()
	r := TaskResult{Task: t, Closure: closure, Status: TaskStatusSkipped}

	if !t.ShouldExecute(closure) {
		common.LogDebug("Skipping revert of task", map[string]interface{}{
			"task": t.Name,
			"host": closure.HostContext.Host.Name,
		})
		return r
	}
	module, ok := GetModule(t.Module)
	if !ok {
		r.Error = fmt.Errorf("module %s not found", t.Module)
		return r
	}

	// Load previous output from history using sync.Map.Load
	previousOutputRaw, found := closure.HostContext.History.Load(t.Name) // Changed from index access
	if !found {
		common.LogWarn("No previous history found for task during revert", map[string]interface{}{
			"task": t.Name,
			"host": closure.HostContext.Host.Name,
		})
		previousOutputRaw = nil
	}

	// Type assert the loaded value to ModuleOutput
	var previousOutput ModuleOutput
	if previousOutputRaw != nil {
		var assertOk bool
		previousOutput, assertOk = previousOutputRaw.(ModuleOutput)
		if !assertOk {
			r.Error = fmt.Errorf("failed to assert previous history type (%T) to ModuleOutput for task %s", previousOutputRaw, t.Name)
			r.Duration = time.Since(startTime)
			return r
		}
	}

	// Pass t.Params.Actual to module.Revert
	// Similar nil check considerations as in ExecuteModule for t.Params.Actual
	r.Output, r.Error = module.Revert(t.Params.Actual, closure, previousOutput, t.RunAs) // Pass potentially nil previousOutput
	duration := time.Since(startTime)
	r.Duration = duration

	return handleResult(&r, t, closure)
}

func handleResult(r *TaskResult, t Task, c *Closure) TaskResult {
	if r.Error != nil {
		r.Status = TaskStatusFailed
		r.Failed = true
	} else if r.Output.Changed() {
		r.Status = TaskStatusChanged
		r.Changed = true
	} else {
		r.Status = TaskStatusOk
	}
	registerVariableIfNeeded(*r, t, c)

	// Evaluate failed_when only if the module execution itself succeeded
	if r.Error == nil && t.FailedWhen != "" {
		// Evaluate the expression, merging host facts and task output facts
		templatedFailedWhen, err := EvaluateExpression(t.FailedWhen, c)
		if err != nil {
			// TODO: should we return a list of errors?
			// Treat evaluation errors as task failure, as we can't determine the condition
			r.Error = fmt.Errorf("error evaluating failed_when condition '%s': %w", t.FailedWhen, err)
			common.LogWarn("Error evaluating failed_when condition, marking task as failed", map[string]interface{}{
				"task":      t.Name,
				"host":      c.HostContext.Host.Name,
				"condition": t.FailedWhen,
				"error":     err.Error(),
			})
			r.Failed = true
		} else {
			// Evaluate truthiness of the result
			conditionMet := IsExpressionTruthy(templatedFailedWhen)
			trimmedResult := strings.TrimSpace(templatedFailedWhen)
			common.DebugOutput("Evaluated failed_when condition %q -> %q: %t",
				t.FailedWhen, trimmedResult, conditionMet)

			if conditionMet {
				// Set the error if the condition is true
				r.Error = fmt.Errorf("failed_when condition '%s' evaluated to true (%s)", t.FailedWhen, trimmedResult)
				r.Status = TaskStatusFailed
				r.Failed = true
			}
		}
	}

	if r.Error != nil && t.IgnoreErrors {
		common.LogWarn("Task failed but error ignored due to ignore_errors=true", map[string]interface{}{
			"task":  t.Name,
			"host":  c.HostContext.Host.Name,
			"error": r.Error.Error(),
		})
		// Wrap the original error in IgnoredTaskError
		r.Error = &IgnoredTaskError{OriginalErr: r.Error}
		r.Failed = true
	}

	if t.ChangedWhen != "" && r.Status != TaskStatusFailed {
		templatedChangedWhen, err := EvaluateExpression(t.ChangedWhen, c)
		if err != nil {
			r.Error = fmt.Errorf("error evaluating changed_when condition '%s': %w", t.ChangedWhen, err)
			r.Failed = true
		}
		if IsExpressionTruthy(templatedChangedWhen) {
			r.Status = TaskStatusChanged
			r.Changed = true
		} else {
			r.Status = TaskStatusOk
			r.Changed = false
		}
	}

	// failed_when/changed_when might depend on results of this task, so we need to evaluate them after registration
	// However, we should update the changed/failed status after evaluating these conditions.
	setTaskStatus(*r, t, c)
	return *r
}
