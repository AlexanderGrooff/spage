package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/jinja-go"
	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
)

type AssertModule struct{}

func (am AssertModule) InputType() reflect.Type {
	return reflect.TypeOf(AssertInput{})
}

func (am AssertModule) OutputType() reflect.Type {
	return reflect.TypeOf(AssertOutput{})
}

// Doc returns module-level documentation rendered into Markdown.
func (am AssertModule) Doc() string {
	return `Assert that given expressions are true. This module evaluates a list of conditions and fails if any of them are false, making it useful for validation and testing.

## Examples

` + "```yaml" + `
- name: Assert variable is defined
  assert:
    that:
      - my_var is defined
    msg: "Variable my_var must be defined"

- name: Multiple assertions
  assert:
    that:
      - ansible_os_family == "RedHat"
      - ansible_distribution_major_version >= "7"
    msg: "This playbook requires RHEL/CentOS 7 or later"

- name: Assert with complex conditions
  assert:
    that:
      - inventory_hostname in groups['webservers']
      - port | int > 0 and port | int < 65536
      - config_file is file

- name: Assert service is running
  assert:
    that:
      - service_status.stdout == "running"
    msg: "Service must be running before proceeding"
` + "```" + `

**Note**: All conditions in the 'that' list must be true for the assertion to pass. If any condition fails, the task will fail with the specified message.
`
}

// ParameterDocs provides rich documentation for assert module inputs.
func (am AssertModule) ParameterDocs() map[string]pkg.ParameterDoc {
	notRequired := false
	return map[string]pkg.ParameterDoc{
		"that": {
			Description: "List of Jinja2 expressions that must evaluate to true. All expressions must pass for the assertion to succeed.",
			Required:    &notRequired,
			Default:     "",
		},
		"msg": {
			Description: "Custom message to display when assertion fails. If not provided, a default message will be shown.",
			Required:    &notRequired,
			Default:     "Assertion failed",
		},
	}
}

type AssertInput struct {
	That []string `yaml:"that"` // List of assertions to evaluate
	Msg  string   `yaml:"msg"`  // Optional message on failure
}

type AssertOutput struct {
	FailedAssertion string // Which assertion failed, if any
	pkg.ModuleOutput
}

func (i AssertInput) ToCode() string {
	// Convert the slice of strings to a Go literal representation
	thatCode := "[]string{"
	for _, assertion := range i.That {
		thatCode += fmt.Sprintf("%q, ", assertion)
	}
	if len(i.That) > 0 {
		thatCode = strings.TrimSuffix(thatCode, ", ")
	}
	thatCode += "}"

	return fmt.Sprintf("modules.AssertInput{That: %s, Msg: %q}",
		thatCode,
		i.Msg,
	)
}

func (i AssertInput) GetVariableUsage() []string {
	var baseVars []string
	for _, assertion := range i.That {
		newVars, err := jinja.ParseVariablesFromExpression(assertion)
		if err == nil {
			for _, v := range newVars {
				base := v
				if idx := strings.IndexAny(base, ".["); idx != -1 {
					base = base[:idx]
				}
				baseVars = append(baseVars, strings.ToLower(base))
			}
		}
	}
	baseVars = append(baseVars, pkg.GetVariableUsageFromTemplate(i.Msg)...)
	// Deduplicate
	uniq := make(map[string]struct{}, len(baseVars))
	out := make([]string, 0, len(baseVars))
	for _, v := range baseVars {
		if v == "" {
			continue
		}
		if _, ok := uniq[v]; ok {
			continue
		}
		uniq[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// HasRevert indicates that the assert module cannot be reverted.
func (i AssertInput) HasRevert() bool {
	return false
}

func (i AssertInput) ProvidesVariables() []string {
	return nil
}

func (i AssertInput) Validate() error {
	if len(i.That) == 0 {
		return fmt.Errorf("missing 'that' input: at least one assertion is required")
	}
	return nil
}

func (o AssertOutput) String() string {
	if o.FailedAssertion != "" {
		return fmt.Sprintf("Assertion failed: %s", o.FailedAssertion)
	}
	return "All assertions passed"
}

func (o AssertOutput) Changed() bool {
	// Assertions do not change state
	return false
}

func (m AssertModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	assertParams, ok := params.(AssertInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected AssertInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected AssertInput, got %T", params)
	}

	output := AssertOutput{}

	for _, assertion := range assertParams.That {
		renderedAssertion, err := pkg.EvaluateExpression(assertion, closure)
		if err != nil {
			output.FailedAssertion = assertion // Use original assertion on render error
			errMsg := fmt.Sprintf("failed to render assertion template %q: %v", assertion, err)
			if assertParams.Msg != "" {
				renderedMsg, renderErr := pkg.TemplateString(assertParams.Msg, closure)
				if renderErr != nil {
					errMsg = fmt.Sprintf("%s (also failed to render custom message: %v)", errMsg, renderErr)
				} else {
					errMsg = fmt.Sprintf("%s: %s", errMsg, renderedMsg)
				}
			}
			return output, fmt.Errorf("%s", errMsg)
		}

		// Evaluate truthiness using the helper function
		assertionPassed := jinja.IsTruthy(renderedAssertion)

		// Log the evaluation
		common.DebugOutput("Evaluated assertion %q -> %v: %t",
			assertion, renderedAssertion, assertionPassed)

		if !assertionPassed { // Use the evaluated truthiness
			output.FailedAssertion = assertion // Use original assertion string
			errMsg := fmt.Sprintf("assertion failed: %q (evaluated to %q)", assertion, renderedAssertion)
			if assertParams.Msg != "" {
				renderedMsg, renderErr := pkg.TemplateString(assertParams.Msg, closure)
				if renderErr != nil {
					errMsg = fmt.Sprintf("%s (also failed to render custom message: %v)", errMsg, renderErr)
				} else {
					errMsg = fmt.Sprintf("%s: %s", renderedMsg, errMsg) // Custom message first
				}
			}
			return output, fmt.Errorf("%s", errMsg)
		}
	}

	return output, nil
}

// Revert for Assert is a no-op as it doesn't change state
func (m AssertModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	common.LogDebug("Revert called for assert module (no-op)", map[string]interface{}{})
	return AssertOutput{}, nil // Return zero value of AssertOutput
}

func init() {
	pkg.RegisterModule("assert", AssertModule{})
	pkg.RegisterModule("ansible.builtin.assert", AssertModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m AssertModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
