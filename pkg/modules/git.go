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
	pkg.ModuleInput
}

type GitOutput struct {
	RevBefore string `yaml:"rev_before"`
	RevAfter  string `yaml:"rev_after"`
	Dest      string `yaml:"dest"`
	pkg.ModuleOutput
}

func (i GitInput) ToCode() string {
	return fmt.Sprintf("modules.GitInput{Repo: %q, Dest: %q, Version: %q}",
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
		return fmt.Errorf("missing required parameters. Repo and Dest should be given")
	}
	return nil
}

func (o GitOutput) String() string {
	return fmt.Sprintf("  rev_before: %q\n  rev_after: %q\n  dest: %q\n", o.RevBefore, o.RevAfter, o.Dest)
}

func (o GitOutput) Changed() bool {
	return o.RevBefore != o.RevAfter
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

func (m GitModule) Execute(params pkg.ModuleInput, c *pkg.HostContext, runAs string) (pkg.ModuleOutput, error) {
	gitParams := params.(GitInput)
	revBefore, err := m.GetCurrentRev(gitParams.Dest, c, runAs)
	if err != nil {
		m.CloneRepo(gitParams.Repo, gitParams.Dest, c, runAs)
	}
	if gitParams.Version != "" {
		m.CheckoutVersion(gitParams.Dest, gitParams.Version, c, runAs)
	}
	revAfter, err := m.GetCurrentRev(gitParams.Dest, c, runAs)
	if err != nil {
		return nil, fmt.Errorf("failed to get current rev of repo %s: %w", gitParams.Dest, err)
	}
	return GitOutput{
		RevBefore: revBefore,
		RevAfter:  revAfter,
		Dest:      gitParams.Dest,
	}, nil
}

func (m GitModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	gitParams := params.(GitInput)
	if previous != nil {
		prev := previous.(GitOutput)
		if prev.Changed() {
			m.CheckoutVersion(gitParams.Dest, prev.RevBefore, c, runAs)
		}
		return GitOutput{
			RevBefore: prev.RevAfter,
			RevAfter:  prev.RevBefore,
			Dest:      gitParams.Dest,
		}, nil
	}
	return GitOutput{Dest: gitParams.Dest}, nil
}

func init() {
	pkg.RegisterModule("git", GitModule{})
}
