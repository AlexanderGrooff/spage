package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
)

// DebugModule implements the logic for the debug module.
type DebugModule struct{}

func (m DebugModule) InputType() reflect.Type {
	return reflect.TypeOf(DebugInput{})
}

func (m DebugModule) OutputType() reflect.Type {
	return reflect.TypeOf(DebugOutput{})
}

// Doc returns module-level documentation rendered into Markdown.
func (m DebugModule) Doc() string {
	return `Print debug information during playbook execution. Use this module to display variable values, messages, or other debugging information.

## Examples

` + "```yaml" + `
- name: Print a simple message
  debug:
    msg: "Hello, World!"

- name: Display variable content
  debug:
    var: ansible_hostname

- name: Print variable with message
  debug:
    msg: "The hostname is {{ ansible_hostname }}"

- name: Debug complex data structures
  debug:
    var: my_dict

- name: Conditional debug
  debug:
    msg: "This will only print if condition is true"
  when: some_condition
` + "```" + `

**Note**: Debug output is always displayed, even in check mode.
`
}

// ParameterDocs provides rich documentation for debug module inputs.
func (m DebugModule) ParameterDocs() map[string]pkg.ParameterDoc {
	notRequired := false
	return map[string]pkg.ParameterDoc{
		"msg": {
			Description: "Message to display. Can be a string, list, or templated content. Cannot be used with 'var'.",
			Required:    &notRequired, // msg is optional (can use var instead)
			Default:     "",
		},
		"var": {
			Description: "Name of variable to display. Cannot be used with 'msg'.",
			Required:    &notRequired, // var is optional (can use msg instead)
			Default:     "",
		},
	}
}

// DebugInput defines the structure for the debug module's input.
// It can take a 'msg' to print directly, or a 'var' to print a variable's content.
type DebugInput struct {
	Msg interface{} `yaml:"msg,omitempty"`
	Var string      `yaml:"var,omitempty"`
}

// DebugOutput provides information about what was printed.
type DebugOutput struct {
	MessagePrinted string
}

// ToCode generates Go code representation of the DebugInput.
func (i DebugInput) ToCode() string {
	var parts []string
	if i.Msg != nil {
		switch v := i.Msg.(type) {
		case string:
			parts = append(parts, fmt.Sprintf("Msg: %#v", v))
		case []interface{}:
			// To generate Go code, we create a slice of strings.
			// The items in YAML are unmarshalled into interface{}, so we format them within an interface{} slice in Go.
			strSlice := make([]string, len(v))
			for i, item := range v {
				strSlice[i] = fmt.Sprintf("%#v", item)
			}
			parts = append(parts, fmt.Sprintf("Msg: []interface{}{%s}", strings.Join(strSlice, ", ")))
		case []string:
			strSlice := make([]string, len(v))
			for i, item := range v {
				strSlice[i] = fmt.Sprintf("%#v", item)
			}
			parts = append(parts, fmt.Sprintf("Msg: []interface{}{%s}", strings.Join(strSlice, ", ")))
		}
	}

	if i.Var != "" {
		parts = append(parts, fmt.Sprintf("Var: %q", i.Var))
	}
	return fmt.Sprintf("modules.DebugInput{%s}", strings.Join(parts, ", "))
}

// GetVariableUsage extracts variables used within the 'msg' or 'var' fields.
func (i DebugInput) GetVariableUsage() []string {
	var vars []string
	if i.Msg != nil {
		switch msg := i.Msg.(type) {
		case string:
			vars = append(vars, pkg.GetVariableUsageFromTemplate(msg)...)
		case []interface{}:
			for _, item := range msg {
				if itemStr, ok := item.(string); ok {
					vars = append(vars, pkg.GetVariableUsageFromTemplate(itemStr)...)
				}
			}
		case []string:
			for _, itemStr := range msg {
				vars = append(vars, pkg.GetVariableUsageFromTemplate(itemStr)...)
			}
		}
	}
	// The 'var' field itself is a variable name, but it could also be a path like 'some_dict.key'
	// For now, we'll treat 'var' as a potential variable to be looked up directly.
	// If 'var' is meant to be templated like '{{ some_var }}', then it's covered by GetVariableUsageFromTemplate.
	// If 'var' refers to a registered variable like 'my_registered_result', it's handled differently.
	// For simplicity, let's assume if 'var' is used, its content (the variable name) might be templated if it contains '{{}}'.
	if strings.Contains(i.Var, "{{") && strings.Contains(i.Var, "}}") {
		vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Var)...)
		// else if i.Var != "": If 'var' does not contain '{{}}', it's treated as a direct variable name.
		// We don't add it to 'vars' here as GetVariableUsage is for template variables.
		// The logic in Execute will handle resolving it from the closure.
	}

	// Deduplicate variables
	uniqueVars := make(map[string]struct{})
	for _, v := range vars {
		uniqueVars[v] = struct{}{}
	}
	result := make([]string, 0, len(uniqueVars))
	for k := range uniqueVars {
		result = append(result, k)
	}
	return result
}

// ProvidesVariables returns an empty list as debug doesn't set new variables.
func (i DebugInput) ProvidesVariables() []string {
	return []string{}
}

// Validate ensures that either 'msg' or 'var' is provided.
func (i DebugInput) Validate() error {
	msgIsSet := i.Msg != nil
	// An empty string for 'msg' is not considered set.
	if msgStr, ok := i.Msg.(string); ok && msgStr == "" {
		msgIsSet = false
	}
	if !msgIsSet && i.Var == "" {
		return fmt.Errorf("either 'msg' or 'var' must be provided to debug module")
	}
	if msgIsSet && i.Var != "" {
		return fmt.Errorf("only one of 'msg' or 'var' can be provided to debug module")
	}
	return nil
}

// HasRevert indicates that debug module does not have a revert action.
func (i DebugInput) HasRevert() bool {
	return true
}

// String provides a human-readable summary of the output.
func (o DebugOutput) String() string {
	return o.MessagePrinted
}

// Changed indicates that the debug module does not change state.
func (o DebugOutput) Changed() bool {
	return false
}

// UnmarshalYAML custom unmarshaler to handle parameters.
func (i *DebugInput) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try unmarshalling into a temporary map to check keys
	tempMap := make(map[string]interface{})
	if err := unmarshal(&tempMap); err != nil {
		var str string
		if unmarshal(&str) == nil {
			i.Msg = str
			return i.Validate()
		}
		return err
	}

	if msg, ok := tempMap["msg"]; ok {
		i.Msg = msg
	}

	if varParam, ok := tempMap["var"]; ok {
		if varStr, ok := varParam.(string); ok {
			i.Var = varStr
		} else {
			return fmt.Errorf("debug 'var' parameter must be a string")
		}
	}

	return i.Validate()
}

// private helper function for shared debug logic
func (m DebugModule) debugAction(params DebugInput, closure *pkg.Closure, action string) (pkg.ModuleOutput, error) {
	var outputMessage string
	suffix := ""
	if action == "revert" {
		suffix = " [revert]"
	}

	processMessage := func(msg string) (string, error) {
		templatedMsg, err := pkg.TemplateString(msg, closure)
		if err != nil {
			common.LogWarn(fmt.Sprintf("Failed to template debug message during %s, printing raw value", action), map[string]interface{}{
				"host":    closure.HostContext.Host.Name,
				"raw_msg": msg,
				"error":   err.Error(),
			})
			return fmt.Sprintf("msg: %s (templating failed: %v)%s", msg, err, suffix), err
		}
		return fmt.Sprintf("msg: %s%s", templatedMsg, suffix), nil
	}

	if params.Msg != nil {
		switch msg := params.Msg.(type) {
		case string:
			outputMessage, _ = processMessage(msg)
		case []interface{}:
			var messages []string
			for _, item := range msg {
				if itemStr, ok := item.(string); ok {
					processedMsg, _ := processMessage(itemStr)
					messages = append(messages, processedMsg)
				}
			}
			outputMessage = strings.Join(messages, "\n")
		case []string:
			var messages []string
			for _, itemStr := range msg {
				processedMsg, _ := processMessage(itemStr)
				messages = append(messages, processedMsg)
			}
			outputMessage = strings.Join(messages, "\n")
		}
		common.LogInfo(outputMessage, map[string]interface{}{"host": closure.HostContext.Host.Name, "module": "debug", "action": action})
	} else if params.Var != "" {
		varName := params.Var
		if strings.Contains(params.Var, "{{") && strings.Contains(params.Var, "}}") {
			resolvedVarName, err := pkg.TemplateString(params.Var, closure)
			if err != nil {
				common.LogWarn(fmt.Sprintf("Failed to template debug 'var' name during %s, using raw value", action), map[string]interface{}{
					"host":    closure.HostContext.Host.Name,
					"raw_var": params.Var,
					"error":   err.Error(),
				})
			} else {
				varName = resolvedVarName
			}
		}

		val, found := closure.HostContext.Facts.Load(varName)
		if found {
			switch v := val.(type) {
			case map[string]interface{}, []interface{}:
				outputMessage = fmt.Sprintf("var: %s = %#v%s", varName, v, suffix)
			default:
				outputMessage = fmt.Sprintf("var: %s = %v%s", varName, val, suffix)
			}
		} else {
			outputMessage = fmt.Sprintf("var: %s (not found)%s", varName, suffix)
		}
	}

	return DebugOutput{MessagePrinted: outputMessage}, nil
}

// Execute prints the message or variable content.
func (m DebugModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	debugParams, ok := params.(DebugInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected DebugInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected DebugInput, got %T", params)
	}
	return m.debugAction(debugParams, closure, "execute")
}

// Revert for debug now mirrors Execute.
func (m DebugModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	debugParams, ok := params.(DebugInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Revert: params is nil, expected DebugInput but got nil")
		}
		return nil, fmt.Errorf("Revert: incorrect parameter type: expected DebugInput, got %T", params)
	}

	// Optional: Log information about the previous state if needed before calling the common action.
	if prevDebugOutput, ok := previous.(DebugOutput); ok {
		common.LogDebug("Reverting debug action", map[string]interface{}{
			"host": closure.HostContext.Host.Name, "module": "debug", "previous_message": prevDebugOutput.MessagePrinted,
		})
	}

	return m.debugAction(debugParams, closure, "revert")
}

func init() {
	pkg.RegisterModule("debug", DebugModule{})
	pkg.RegisterModule("ansible.builtin.debug", DebugModule{})
}

// ParameterAliases defines aliases if needed.
func (m DebugModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for debug
}
