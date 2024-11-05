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
	Name    string `yaml:"name"`
	State   string `yaml:"state"`
	Enabled bool   `yaml:"enabled"`
	pkg.ModuleInput
}

type SystemdOutput struct {
	PreviousState SystemdState `yaml:"previous_state"`
	CurrentState  SystemdState `yaml:"current_state"`
	pkg.ModuleOutput
}

func (i SystemdInput) ToCode() string {
	return fmt.Sprintf("modules.SystemdInput{Name: %q, State: %q, Enabled: %v}",
		i.Name,
		i.State,
		i.Enabled,
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
	return fmt.Sprintf("  previous_state: %q\n  current_state: %q\n", o.PreviousState, o.CurrentState)
}

func (o SystemdOutput) Changed() bool {
	return o.PreviousState != o.CurrentState
}

func (m SystemdModule) getCurrentState(name string, c *pkg.HostContext, runAs string) (SystemdState, error) {
	stdout, _, err := c.RunCommand(fmt.Sprintf("systemctl is-enabled %s", name), runAs)
	if err != nil {
		return SystemdState{}, err
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

func (m SystemdModule) Execute(params pkg.ModuleInput, c *pkg.HostContext, runAs string) (pkg.ModuleOutput, error) {
	systemdParams := params.(SystemdInput)
	stateBeforeExecute, err := m.getCurrentState(systemdParams.Name, c, runAs)
	currentState := SystemdState{}
	if err != nil {
		return nil, err
	}
	if systemdParams.Enabled && !stateBeforeExecute.Enabled {
		err := m.Enable(systemdParams.Name, c, runAs)
		if err != nil {
			return nil, err
		}
		currentState.Enabled = true
	}
	if systemdParams.State == "started" && !stateBeforeExecute.Started {
		err := m.Start(systemdParams.Name, c, runAs)
		if err != nil {
			return nil, err
		}
		currentState.Started = true
	}
	return SystemdOutput{
		PreviousState: stateBeforeExecute,
		CurrentState:  currentState,
	}, nil
}

func (m SystemdModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	systemdParams := params.(SystemdInput)
	originalState := previous.(SystemdOutput).PreviousState
	stateBeforeRevert, err := m.getCurrentState(systemdParams.Name, c, runAs)
	if err != nil {
		return nil, err
	}
	if systemdParams.Enabled && !originalState.Enabled {
		err := m.Disable(systemdParams.Name, c, runAs)
		if err != nil {
			return nil, err
		}
	}
	stateAfterRevert, err := m.getCurrentState(systemdParams.Name, c, runAs)
	if err != nil {
		return nil, err
	}
	return SystemdOutput{
		PreviousState: stateBeforeRevert,
		CurrentState:  stateAfterRevert,
	}, nil
}

func init() {
	pkg.RegisterModule("systemd", SystemdModule{})
}
