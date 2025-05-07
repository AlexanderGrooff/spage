package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
)

type GitModule struct{}

func (sm GitModule) InputType() reflect.Type {
	return reflect.TypeOf(GitInput{})
}

func (sm GitModule) OutputType() reflect.Type {
	return reflect.TypeOf(GitOutput{})
}

type GitInput struct {
	Repo    string `yaml:"repo"`
	Dest    string `yaml:"dest"`
	Version string `yaml:"version"`
}

type GitOutput struct {
	Rev  pkg.RevertableChange[string]
	Dest string
	pkg.ModuleOutput
}

func (i GitInput) ToCode() string {
	return fmt.Sprintf("modules.GitInput{Repo: %q, Dst: %q, Version: %q}",
		i.Repo,
		i.Dest,
		i.Version,
	)
}
func (i GitInput) GetVariableUsage() []string {
	appendVars := func(vars []string, str string) []string {
		return append(vars, pkg.GetVariableUsageFromTemplate(str)...)
	}
	vars := pkg.GetVariableUsageFromTemplate(i.Repo)
	vars = appendVars(vars, i.Dest)
	vars = appendVars(vars, i.Version)
	return vars
}

func (i GitInput) Validate() error {
	if i.Repo == "" || i.Dest == "" {
		return fmt.Errorf("missing required parameters. Repo and Dst should be given")
	}
	return nil
}

func (i GitInput) ProvidesVariables() []string {
	return nil
}

func (i GitInput) HasRevert() bool {
	return true
}

func (o GitOutput) String() string {
	return fmt.Sprintf("  rev_before: %q\n  rev_after: %q\n  dest: %q\n", o.Rev.Before, o.Rev.After, o.Dest)
}

func (o GitOutput) Changed() bool {
	return o.Rev.Changed()
}

func (m GitModule) GetCurrentRev(dest string, c *pkg.HostContext, runAs string) (string, error) {
	stdout, _, err := c.RunCommand(fmt.Sprintf("git -C %s rev-parse HEAD", dest), runAs)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout), nil
}

func (m GitModule) CloneRepo(repo, dest string, c *pkg.HostContext, runAs string) error {
	_, _, err := c.RunCommand(fmt.Sprintf("git clone %s %s", repo, dest), runAs)
	if err != nil {
		return fmt.Errorf("failed to clone repo %s to %s: %w", repo, dest, err)
	}
	return nil
}

func (m GitModule) CheckoutVersion(dest, version string, c *pkg.HostContext, runAs string) (string, error) {
	_, stderr, err := c.RunCommand(fmt.Sprintf("git -C %s checkout %s", dest, version), runAs)
	if err != nil {
		return "", fmt.Errorf("failed to checkout version %s in repo %s: %w\nstderr: %s", version, dest, err, stderr)
	}
	return "", nil
}

func (m GitModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	gitParams, ok := params.(GitInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected GitInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected GitInput, got %T", params)
	}

	if err := gitParams.Validate(); err != nil {
		return nil, err
	}

	revBefore, err := m.GetCurrentRev(gitParams.Dest, closure.HostContext, runAs)
	if err != nil {
		m.CloneRepo(gitParams.Repo, gitParams.Dest, closure.HostContext, runAs)
	}
	if gitParams.Version != "" {
		m.CheckoutVersion(gitParams.Dest, gitParams.Version, closure.HostContext, runAs)
	}
	revAfter, err := m.GetCurrentRev(gitParams.Dest, closure.HostContext, runAs)
	if err != nil {
		return nil, fmt.Errorf("failed to get current rev of repo %s: %w", gitParams.Dest, err)
	}
	return GitOutput{
		Rev: pkg.RevertableChange[string]{
			Before: revBefore,
			After:  revAfter,
		},
		Dest: gitParams.Dest,
	}, nil
}

func (m GitModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	gitParams, ok := params.(GitInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Revert: params is nil, expected GitInput but got nil")
		}
		return nil, fmt.Errorf("Revert: incorrect parameter type: expected GitInput, got %T", params)
	}

	// Revert for git module: if the repo was updated, try to revert to the 'previous' state.
	// A more robust revert (e.g., removing a cloned repo if original state was absent) is not fully implemented.
	if previous != nil {
		prevOutput, ok := previous.(GitOutput)
		if !ok {
			return nil, fmt.Errorf("Revert: previous output was not of type GitOutput, got %T", previous)
		}
		if prevOutput.Changed() && prevOutput.Rev.Before != "" {
			common.LogInfo("Reverting git repo", map[string]interface{}{"dest": gitParams.Dest, "reverting_to_rev": prevOutput.Rev.Before})
			_, err := m.CheckoutVersion(gitParams.Dest, prevOutput.Rev.Before, closure.HostContext, runAs)
			if err != nil {
				return nil, fmt.Errorf("Revert: failed to checkout previous version %s for %s: %w", prevOutput.Rev.Before, gitParams.Dest, err)
			}
			// After successful revert, the new state's "Before" is the one we just reverted to.
			// And the new state's "After" is what it was before this revert (prevOutput.Rev.After).
			// This might be confusing. For now, let's represent the reverted state.
			currentRevAfterRevert, _ := m.GetCurrentRev(gitParams.Dest, closure.HostContext, runAs)
			return GitOutput{
				Rev: pkg.RevertableChange[string]{
					Before: prevOutput.Rev.After,  // State before this revert operation
					After:  currentRevAfterRevert, // State after this revert operation (should be prevOutput.Rev.Before)
				},
				Dest: gitParams.Dest,
			}, nil
		} else {
			// Previous output existed but didn't change, or no before revision to revert to.
			common.LogInfo("Revert: No change detected in previous GitOutput or no before revision, no revert action taken.", map[string]interface{}{"dest": gitParams.Dest})
			return GitOutput{Dest: gitParams.Dest, Rev: prevOutput.Rev}, nil // Return current state as unchanged
		}
	}
	// No previous output, cannot determine how to revert.
	common.LogWarn("Revert: No previous GitOutput, cannot perform revert.", map[string]interface{}{"dest": gitParams.Dest})
	return GitOutput{Dest: gitParams.Dest}, nil // Indicate no change as we couldn't revert.
}

func init() {
	pkg.RegisterModule("git", GitModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m GitModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
