package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
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

func (m GitModule) Execute(params pkg.ModuleInput, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	gitParams := params.(GitInput)
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

func (m GitModule) Revert(params pkg.ModuleInput, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	gitParams := params.(GitInput)
	if previous != nil {
		prev := previous.(GitOutput)
		if prev.Changed() {
			m.CheckoutVersion(gitParams.Dest, prev.Rev.Before, closure.HostContext, runAs)
		}
		return GitOutput{
			Rev: pkg.RevertableChange[string]{
				Before: prev.Rev.After,
				After:  prev.Rev.Before,
			},
			Dest: gitParams.Dest,
		}, nil
	}
	return GitOutput{Dest: gitParams.Dest}, nil
}

func init() {
	pkg.RegisterModule("git", GitModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m GitModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
