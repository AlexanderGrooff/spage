package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
)

type SystemdModule struct{}

func (sm SystemdModule) InputType() reflect.Type {
	return reflect.TypeOf(SystemdInput{})
}

func (sm SystemdModule) OutputType() reflect.Type {
	return reflect.TypeOf(SystemdOutput{})
}

type SystemdState struct {
	Enabled bool `yaml:"enabled"`
	Started bool `yaml:"started"`
}

func (s SystemdState) String() string {
	return fmt.Sprintf("enabled=%v started=%v", s.Enabled, s.Started)
}

func (s SystemdState) Equal(other SystemdState) bool {
	return s.Enabled == other.Enabled && s.Started == other.Started
}

type SystemdInput struct {
	Name         string `yaml:"name"`
	State        string `yaml:"state"`
	Enabled      bool   `yaml:"enabled"`
	DaemonReload bool   `yaml:"daemon_reload"`
	pkg.ModuleInput
}

type SystemdOutput struct {
	State pkg.RevertableChange[SystemdState]
	pkg.ModuleOutput
}

func (i SystemdInput) ToCode() string {
	return fmt.Sprintf("modules.SystemdInput{Name: %q, State: %q, Enabled: %v, DaemonReload: %v}",
		i.Name,
		i.State,
		i.Enabled,
		i.DaemonReload,
	)
}

func (i SystemdInput) GetVariableUsage() []string {
	return append(pkg.GetVariableUsageFromTemplate(i.Name), pkg.GetVariableUsageFromTemplate(i.State)...)
}

func (i SystemdInput) Validate() error {
	if i.Name == "" {
		return fmt.Errorf("missing required parameter. Name should be given")
	}
	if i.State == "" {
		return fmt.Errorf("missing required parameter. State should be given")
	}
	return nil
}

func (o SystemdOutput) String() string {
	return fmt.Sprintf("  state: %q\n", o.State)
}

func (o SystemdOutput) Changed() bool {
	return o.State.Changed()
}

func (m SystemdModule) getCurrentState(name string, c *pkg.HostContext, runAs string) (SystemdState, error) {
	stdout, _, err := c.RunCommand(fmt.Sprintf("systemctl is-enabled %s", name), runAs)
	if err != nil {
		return SystemdState{Enabled: false}, nil
	}
	return SystemdState{Enabled: strings.TrimSpace(stdout) == "enabled"}, nil
}

func (m SystemdModule) Enable(name string, c *pkg.HostContext, runAs string) error {
	_, _, err := c.RunCommand(fmt.Sprintf("systemctl enable %s", name), runAs)
	return err
}

func (m SystemdModule) Start(name string, c *pkg.HostContext, runAs string) error {
	_, _, err := c.RunCommand(fmt.Sprintf("systemctl start %s", name), runAs)
	return err
}

func (m SystemdModule) Stop(name string, c *pkg.HostContext, runAs string) error {
	_, _, err := c.RunCommand(fmt.Sprintf("systemctl stop %s", name), runAs)
	return err
}

func (m SystemdModule) Disable(name string, c *pkg.HostContext, runAs string) error {
	_, _, err := c.RunCommand(fmt.Sprintf("systemctl disable %s", name), runAs)
	return err
}

func (m SystemdModule) DaemonReload(c *pkg.HostContext, runAs string) error {
	_, _, err := c.RunCommand("systemctl daemon-reload", runAs)
	return err
}

func (m SystemdModule) Execute(params pkg.ModuleInput, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	systemdParams := params.(SystemdInput)
	stateBeforeExecute, err := m.getCurrentState(systemdParams.Name, closure.HostContext, runAs)
	if err != nil {
		return SystemdOutput{}, err
	}
	if systemdParams.DaemonReload {
		err := m.DaemonReload(closure.HostContext, runAs)
		if err != nil {
			return SystemdOutput{}, err
		}
	}
	if systemdParams.Enabled && !stateBeforeExecute.Enabled {
		err := m.Enable(systemdParams.Name, closure.HostContext, runAs)
		if err != nil {
			return SystemdOutput{}, err
		}
	}
	if systemdParams.State == "started" && !stateBeforeExecute.Started {
		err := m.Start(systemdParams.Name, closure.HostContext, runAs)
		if err != nil {
			return SystemdOutput{}, err
		}
	}
	currentState, err := m.getCurrentState(systemdParams.Name, closure.HostContext, runAs)
	if err != nil {
		return SystemdOutput{}, err
	}
	if systemdParams.State == "started" && !currentState.Started {
		return SystemdOutput{}, fmt.Errorf("failed to start service %q", systemdParams.Name)
	}
	return SystemdOutput{
		State: pkg.RevertableChange[SystemdState]{
			Before: stateBeforeExecute,
			After:  currentState,
		},
	}, nil
}

func (m SystemdModule) Revert(params pkg.ModuleInput, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	systemdParams := params.(SystemdInput)
	originalState := previous.(SystemdOutput).State.Before
	stateBeforeRevert, err := m.getCurrentState(systemdParams.Name, closure.HostContext, runAs)
	if err != nil {
		return SystemdOutput{}, err
	}
	if systemdParams.DaemonReload {
		err := m.DaemonReload(closure.HostContext, runAs)
		if err != nil {
			return SystemdOutput{}, err
		}
	}
	if systemdParams.Enabled && !originalState.Enabled {
		err := m.Disable(systemdParams.Name, closure.HostContext, runAs)
		if err != nil {
			return SystemdOutput{}, err
		}
	}
	stateAfterRevert, err := m.getCurrentState(systemdParams.Name, closure.HostContext, runAs)
	if err != nil {
		return SystemdOutput{}, err
	}
	return SystemdOutput{
		State: pkg.RevertableChange[SystemdState]{
			Before: stateBeforeRevert,
			After:  stateAfterRevert,
		},
	}, nil
}

func init() {
	pkg.RegisterModule("systemd", SystemdModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m SystemdModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
