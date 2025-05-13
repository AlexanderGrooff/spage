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

// DebugInput defines the structure for the debug module's input.
// It can take a 'msg' to print directly, or a 'var' to print a variable's content.
type DebugInput struct {
	Msg string `yaml:"msg,omitempty"`
	Var string `yaml:"var,omitempty"`
}

// DebugOutput provides information about what was printed.
type DebugOutput struct {
	MessagePrinted string
}

// ToCode generates Go code representation of the DebugInput.
func (i DebugInput) ToCode() string {
	var parts []string
	if i.Msg != "" {
		parts = append(parts, fmt.Sprintf("Msg: %q", i.Msg))
	}
	if i.Var != "" {
		parts = append(parts, fmt.Sprintf("Var: %q", i.Var))
	}
	return fmt.Sprintf("modules.DebugInput{%s}", strings.Join(parts, ", "))
}

// GetVariableUsage extracts variables used within the 'msg' or 'var' fields.
func (i DebugInput) GetVariableUsage() []string {
	var vars []string
	if i.Msg != "" {
		vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Msg)...)
	}
	// The 'var' field itself is a variable name, but it could also be a path like 'some_dict.key'
	// For now, we'll treat 'var' as a potential variable to be looked up directly.
	// If 'var' is meant to be templated like '{{ some_var }}', then it's covered by GetVariableUsageFromTemplate.
	// If 'var' refers to a registered variable like 'my_registered_result', it's handled differently.
	// For simplicity, let's assume if 'var' is used, its content (the variable name) might be templated if it contains '{{}}'.
	if strings.Contains(i.Var, "{{") && strings.Contains(i.Var, "}}") {
		vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Var)...)
	} else if i.Var != "" {
		// If 'var' does not contain '{{}}', it's treated as a direct variable name.
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
	if i.Msg == "" && i.Var == "" {
		return fmt.Errorf("either 'msg' or 'var' must be provided to debug module")
	}
	if i.Msg != "" && i.Var != "" {
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
		return err
	}

	if msg, ok := tempMap["msg"]; ok {
		if msgStr, ok := msg.(string); ok {
			i.Msg = msgStr
		} else {
			return fmt.Errorf("debug 'msg' parameter must be a string")
		}
	}

	if varParam, ok := tempMap["var"]; ok {
		if varStr, ok := varParam.(string); ok {
			i.Var = varStr
		} else {
			return fmt.Errorf("debug 'var' parameter must be a string")
		}
	}

	// Ansible also allows 'verbosity' parameter, but we'll keep it simple for now.

	return i.Validate() // Validate after parsing
}

// private helper function for shared debug logic
func (m DebugModule) debugAction(params DebugInput, closure *pkg.Closure, action string) (pkg.ModuleOutput, error) {
	var outputMessage string
	suffix := ""
	if action == "revert" {
		suffix = " [revert]"
	}

	if params.Msg != "" {
		templatedMsg, err := pkg.TemplateString(params.Msg, closure)
		if err != nil {
			common.LogWarn(fmt.Sprintf("Failed to template debug 'msg' during %s, printing raw value", action), map[string]interface{}{
				"host":    closure.HostContext.Host.Name,
				"raw_msg": params.Msg,
				"error":   err.Error(),
			})
			outputMessage = fmt.Sprintf("msg: %s (templating failed: %v)%s", params.Msg, err, suffix)
		} else {
			outputMessage = fmt.Sprintf("msg: %s%s", templatedMsg, suffix)
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
		common.LogInfo(outputMessage, map[string]interface{}{"host": closure.HostContext.Host.Name, "module": "debug", "variable": varName, "action": action})
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
