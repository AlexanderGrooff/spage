package modules

import (
	"fmt"
	"reflect"
	"strings"

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
	var vars []string
	for _, assertion := range i.That {
		vars = append(vars, pkg.GetVariablesFromExpression(assertion)...)
	}
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Msg)...)
	return vars
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
		// TODO: Implement a more robust expression evaluation engine like CEL or leverage existing 'when' logic.
		// For now, we'll do a simple check: treat the string as a boolean.
		// Render any variables first
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
			return output, fmt.Errorf(errMsg)
		}

		// Evaluate truthiness using the helper function
		assertionPassed := pkg.IsExpressionTruthy(renderedAssertion)
		trimmedResult := strings.TrimSpace(renderedAssertion) // Still needed for logging

		// Log the evaluation
		common.DebugOutput("Evaluated assertion %q -> %q: %t",
			assertion, trimmedResult, assertionPassed)

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
			return output, fmt.Errorf(errMsg)
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
}

// ParameterAliases defines aliases for module parameters.
func (m AssertModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
