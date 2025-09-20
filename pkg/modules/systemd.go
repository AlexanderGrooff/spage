package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
)

type SystemdModule struct{}

func (sm SystemdModule) InputType() reflect.Type {
	return reflect.TypeOf(SystemdInput{})
}

func (sm SystemdModule) OutputType() reflect.Type {
	return reflect.TypeOf(SystemdOutput{})
}

// Doc returns module-level documentation rendered into Markdown.
func (sm SystemdModule) Doc() string {
	return `Manage systemd services. This module can start, stop, enable, disable services and reload the systemd daemon configuration.

## Examples

` + "```yaml" + `
- name: Start and enable nginx
  systemd:
    name: nginx
    state: started
    enabled: true

- name: Stop a service
  systemd:
    name: apache2
    state: stopped

- name: Reload systemd daemon
  systemd:
    daemon_reload: true

- name: Enable service without starting
  systemd:
    name: postgresql
    enabled: true
` + "```" + `

**Note**: This module requires systemd to be the init system on the target host.
`
}

// ParameterDocs provides rich documentation for systemd module inputs.
func (sm SystemdModule) ParameterDocs() map[string]pkg.ParameterDoc {
	optional := false
	required := true
	return map[string]pkg.ParameterDoc{
		"name": {
			Description: "Name of the systemd service to manage.",
			Required:    &required,
			Default:     "",
		},
		"state": {
			Description: "Desired state of the service.",
			Required:    &required,
			Default:     "",
			Choices:     []string{"started", "stopped", "restarted", "reloaded"},
		},
		"enabled": {
			Description: "Whether the service should be enabled to start at boot.",
			Required:    &optional,
			Default:     "",
			Choices:     []string{"true", "false"},
		},
		"daemon_reload": {
			Description: "Reload systemd daemon configuration before managing the service.",
			Required:    &optional,
			Default:     "false",
			Choices:     []string{"true", "false"},
		},
	}
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

func (i SystemdInput) HasRevert() bool {
	return true
}

func (i SystemdInput) ProvidesVariables() []string {
	return nil
}

func (o SystemdOutput) String() string {
	return fmt.Sprintf("  state: %q\n", o.State)
}

func (o SystemdOutput) Changed() bool {
	return o.State.Changed()
}

func (m SystemdModule) getCurrentState(name string, c *pkg.HostContext, runAs string) (SystemdState, error) {
	_, stdout, _, err := c.RunCommand(fmt.Sprintf("systemctl is-enabled %s", name), runAs)
	if err != nil {
		return SystemdState{Enabled: false}, nil
	}
	return SystemdState{Enabled: strings.TrimSpace(stdout) == "enabled"}, nil
}

func (m SystemdModule) Enable(name string, c *pkg.HostContext, runAs string) error {
	_, _, _, err := c.RunCommand(fmt.Sprintf("systemctl enable %s", name), runAs)
	return err
}

func (m SystemdModule) Start(name string, c *pkg.HostContext, runAs string) error {
	_, _, _, err := c.RunCommand(fmt.Sprintf("systemctl start %s", name), runAs)
	return err
}

func (m SystemdModule) Stop(name string, c *pkg.HostContext, runAs string) error {
	_, _, _, err := c.RunCommand(fmt.Sprintf("systemctl stop %s", name), runAs)
	return err
}

func (m SystemdModule) Disable(name string, c *pkg.HostContext, runAs string) error {
	_, _, _, err := c.RunCommand(fmt.Sprintf("systemctl disable %s", name), runAs)
	return err
}

func (m SystemdModule) DaemonReload(c *pkg.HostContext, runAs string) error {
	_, _, _, err := c.RunCommand("systemctl daemon-reload", runAs)
	return err
}

func (m SystemdModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	systemdParams, ok := params.(SystemdInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected SystemdInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected SystemdInput, got %T", params)
	}

	if err := systemdParams.Validate(); err != nil {
		return nil, err
	}

	stateBeforeExecute, err := m.getCurrentState(systemdParams.Name, closure.HostContext, runAs)
	if err != nil {
		return SystemdOutput{}, err
	}
	checkMode := closure.IsCheckMode()
	desired := stateBeforeExecute
	if systemdParams.DaemonReload {
		if checkMode {
			common.LogDebug("Would reload systemd daemon", map[string]interface{}{
				"host": closure.HostContext.Host.Name,
			})
			// no-op in check mode
		} else {
			if err := m.DaemonReload(closure.HostContext, runAs); err != nil {
				return SystemdOutput{}, err
			}
		}
	}
	if systemdParams.Enabled && !stateBeforeExecute.Enabled {
		if checkMode {
			common.LogDebug("Would enable service %s", map[string]interface{}{
				"host":    closure.HostContext.Host.Name,
				"service": systemdParams.Name,
			})
			desired.Enabled = true
		} else {
			if err := m.Enable(systemdParams.Name, closure.HostContext, runAs); err != nil {
				return SystemdOutput{}, err
			}
		}
	}
	if systemdParams.State == "started" && !stateBeforeExecute.Started {
		if checkMode {
			common.LogDebug("Would start service %s", map[string]interface{}{
				"host":    closure.HostContext.Host.Name,
				"service": systemdParams.Name,
			})
			desired.Started = true
		} else {
			if err := m.Start(systemdParams.Name, closure.HostContext, runAs); err != nil {
				return SystemdOutput{}, err
			}
		}
	}
	var currentState SystemdState
	if checkMode {
		currentState = desired
	} else {
		var gerr error
		currentState, gerr = m.getCurrentState(systemdParams.Name, closure.HostContext, runAs)
		if gerr != nil {
			return SystemdOutput{}, gerr
		}
		if systemdParams.State == "started" && !currentState.Started {
			return SystemdOutput{}, fmt.Errorf("failed to start service %q", systemdParams.Name)
		}
	}
	return SystemdOutput{
		State: pkg.RevertableChange[SystemdState]{
			Before: stateBeforeExecute,
			After:  currentState,
		},
	}, nil
}

func (m SystemdModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	systemdParams, ok := params.(SystemdInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Revert: params is nil, expected SystemdInput but got nil")
		}
		return nil, fmt.Errorf("Revert: incorrect parameter type: expected SystemdInput, got %T", params)
	}

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
	pkg.RegisterModule("ansible.builtin.systemd", SystemdModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m SystemdModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
