package modules

import (
	"fmt"
	"reflect"
	"strconv"
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
	pkg.ModuleInput
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
		vars = append(vars, pkg.GetVariableUsageFromTemplate(assertion)...)
	}
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Msg)...)
	return vars
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

func (m AssertModule) Execute(params pkg.ModuleInput, c *pkg.HostContext, runAs string) (pkg.ModuleOutput, error) {
	p := params.(AssertInput)
	output := AssertOutput{}

	for _, assertion := range p.That {
		// TODO: Implement a more robust expression evaluation engine like CEL or leverage existing 'when' logic.
		// For now, we'll do a simple check: treat the string as a boolean.
		// Render any variables first
		renderedAssertion, err := pkg.TemplateString(assertion, c.Facts)
		if err != nil {
			output.FailedAssertion = assertion // Use original assertion on render error
			errMsg := fmt.Sprintf("failed to render assertion template %q: %v", assertion, err)
			if p.Msg != "" {
				renderedMsg, renderErr := pkg.TemplateString(p.Msg, c.Facts)
				if renderErr != nil {
					errMsg = fmt.Sprintf("%s (also failed to render custom message: %v)", errMsg, renderErr)
				} else {
					errMsg = fmt.Sprintf("%s: %s", errMsg, renderedMsg)
				}
			}
			return output, fmt.Errorf(errMsg)
		}

		// Simple boolean evaluation
		result, err := strconv.ParseBool(strings.TrimSpace(renderedAssertion))
		if err != nil || !result {
			output.FailedAssertion = assertion // Use original assertion string
			errMsg := fmt.Sprintf("assertion failed: %q (evaluated to %q)", assertion, renderedAssertion)
			if p.Msg != "" {
				renderedMsg, renderErr := pkg.TemplateString(p.Msg, c.Facts)
				if renderErr != nil {
					errMsg = fmt.Sprintf("%s (also failed to render custom message: %v)", errMsg, renderErr)
				} else {
					errMsg = fmt.Sprintf("%s: %s", renderedMsg, errMsg) // Custom message first
				}
			}
			return output, fmt.Errorf(errMsg)
		}
	}

	common.DebugOutput("All assertions passed")
	return output, nil
}

// Revert for Assert is a no-op as it doesn't change state
func (m AssertModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	common.DebugOutput("Revert called for assert module (no-op)")
	return AssertOutput{}, nil
}

func init() {
	pkg.RegisterModule("assert", AssertModule{})
}
