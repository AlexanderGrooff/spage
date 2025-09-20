package modules

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
)

// FailModule implements the logic for the fail module.
type FailModule struct{}

func (m FailModule) InputType() reflect.Type {
	return reflect.TypeOf(FailInput{})
}

func (m FailModule) OutputType() reflect.Type {
	return reflect.TypeOf(FailOutput{})
}

// Doc returns module-level documentation rendered into Markdown.
func (m FailModule) Doc() string {
	return `Fail the playbook execution with a custom message. This module is useful for implementing custom validation logic and stopping execution when certain conditions are not met.

## Examples

` + "```yaml" + `
- name: Fail with a simple message
  fail:
    msg: "This playbook requires root privileges"

- name: Conditional failure
  fail:
    msg: "Variable 'required_var' is not defined"
  when: required_var is not defined

- name: Fail with templated message
  fail:
    msg: "Service {{ service_name }} is not running"
  when: service_status != "running"

- name: Validation failure
  fail:
    msg: "Invalid configuration: port must be between 1 and 65535"
  when: port < 1 or port > 65535
` + "```" + `

**Note**: The fail module will immediately stop playbook execution and display the specified message.
`
}

// ParameterDocs provides rich documentation for fail module inputs.
func (m FailModule) ParameterDocs() map[string]pkg.ParameterDoc {
	notRequired := false
	return map[string]pkg.ParameterDoc{
		"msg": {
			Description: "The failure message to display when the module executes. Can include Jinja2 templating.",
			Required:    &notRequired,
			Default:     "Failed as requested from task",
		},
	}
}

// FailInput defines the structure for the fail module's input.
// It takes a 'msg' to be used as the failure message.
type FailInput struct {
	Msg string `yaml:"msg"`
}

// FailOutput provides information about the failure.
type FailOutput struct {
	FailedMessage string
}

// ToCode generates Go code representation of the FailInput.
func (i FailInput) ToCode() string {
	return fmt.Sprintf("modules.FailInput{Msg: %q}", i.Msg)
}

// GetVariableUsage extracts variables used within the 'msg' field.
func (i FailInput) GetVariableUsage() []string {
	var vars []string
	if i.Msg != "" {
		vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Msg)...)
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

// ProvidesVariables returns an empty list as fail doesn't set new variables.
func (i FailInput) ProvidesVariables() []string {
	return []string{}
}

// Validate ensures that 'msg' is provided.
func (i FailInput) Validate() error {
	if i.Msg == "" {
		return fmt.Errorf("'msg' parameter is required for fail module")
	}
	return nil
}

// HasRevert indicates that fail module does not have a specific revert action.
// The failure itself might trigger a broader revert of previous tasks.
func (i FailInput) HasRevert() bool {
	return false
}

// String provides a human-readable summary of the output.
func (o FailOutput) String() string {
	return o.FailedMessage
}

// Changed indicates that the fail module does not change state.
func (o FailOutput) Changed() bool {
	return false
}

// Execute always returns an error with the provided message.
func (m FailModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	failParams, ok := params.(FailInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected FailInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected FailInput, got %T", params)
	}

	finalMsg := failParams.Msg // Default to original message
	templatedMsg, err := pkg.TemplateString(failParams.Msg, closure)
	if err != nil {
		common.LogWarn("Failed to template fail 'msg', using raw value for failure message", map[string]interface{}{
			"host":    closure.HostContext.Host.Name,
			"raw_msg": failParams.Msg,
			"error":   err.Error(),
		})
		// finalMsg is already failParams.Msg, so no change needed here for the error itself.
	} else {
		finalMsg = templatedMsg // Use templated message if successful
	}

	common.LogError("Task intentionally failed by 'fail' module", map[string]interface{}{
		"host":    closure.HostContext.Host.Name,
		"module":  "fail",
		"message": finalMsg, // Log the message that will be used for the error
	})
	return FailOutput{FailedMessage: finalMsg}, errors.New(finalMsg)
}

// Revert for fail is a no-op. The failure itself is the primary action.
func (m FailModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	common.LogDebug("Revert called for fail module (no-op)", map[string]interface{}{})
	// Even though it's a no-op, if there was a previous state from an Execute (hypothetically),
	// we might want to return it. But FailOutput is simple.
	// Returning a new empty output signifies no change by revert itself.
	if prevFailOutput, ok := previous.(FailOutput); ok {
		return prevFailOutput, nil // Return the message that caused the original failure
	}
	return FailOutput{FailedMessage: "(fail module revert - no operation performed)"}, nil
}

func init() {
	pkg.RegisterModule("fail", FailModule{})
	pkg.RegisterModule("ansible.builtin.fail", FailModule{})
}

// ParameterAliases defines aliases if needed.
func (m FailModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for fail
}
