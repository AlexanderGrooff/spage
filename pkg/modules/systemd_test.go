package modules

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemdModule_Execute(t *testing.T) {
	tests := []struct {
		name       string
		input      SystemdInput
		mockOutput map[string]struct {
			stdout string
			stderr string
			err    error
		}
		wantOutput SystemdOutput
		wantErr    bool
	}{
		{
			name: "enable and start service",
			input: SystemdInput{
				Name:         "test.service",
				State:        "started",
				Enabled:      true,
				DaemonReload: true,
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"systemctl is-enabled test.service": {
					stdout: "disabled",
				},
				"systemctl daemon-reload": {
					stdout: "",
					stderr: "",
				},
				"systemctl enable test.service": {
					stdout: "",
					stderr: "",
				},
				"systemctl start test.service": {
					stdout: "",
					stderr: "",
				},
			},
			wantOutput: SystemdOutput{
				PreviousState: SystemdState{
					Enabled: false,
					Started: false,
				},
				CurrentState: SystemdState{
					Enabled: true,
					Started: true,
				},
			},
		},
		{
			name: "service already enabled and started",
			input: SystemdInput{
				Name:         "test.service",
				State:        "started",
				Enabled:      true,
				DaemonReload: false,
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"systemctl is-enabled test.service": {
					stdout: "enabled",
				},
			},
			wantOutput: SystemdOutput{
				PreviousState: SystemdState{
					Enabled: true,
					Started: false,
				},
				CurrentState: SystemdState{
					Enabled: true,
					Started: false,
				},
			},
		},
		{
			name: "daemon reload failure",
			input: SystemdInput{
				Name:         "test.service",
				State:        "started",
				Enabled:      true,
				DaemonReload: true,
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"systemctl is-enabled test.service": {
					stdout: "disabled",
				},
				"systemctl daemon-reload": {
					stderr: "Failed to reload daemon",
					err:    fmt.Errorf("daemon reload failed"),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := newMockHostContext()

			module := SystemdModule{}
			output, err := module.Execute(tt.input, mockCtx, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			systemdOutput := output.(SystemdOutput)
			assert.Equal(t, tt.wantOutput.PreviousState, systemdOutput.PreviousState)
			assert.Equal(t, tt.wantOutput.CurrentState, systemdOutput.CurrentState)
		})
	}
}

func TestSystemdModule_Revert(t *testing.T) {
	tests := []struct {
		name       string
		input      SystemdInput
		previous   SystemdOutput
		mockOutput map[string]struct {
			stdout string
			stderr string
			err    error
		}
		wantOutput SystemdOutput
		wantErr    bool
	}{
		{
			name: "revert enable and start",
			input: SystemdInput{
				Name:         "test.service",
				State:        "started",
				Enabled:      true,
				DaemonReload: true,
			},
			previous: SystemdOutput{
				PreviousState: SystemdState{
					Enabled: false,
					Started: false,
				},
				CurrentState: SystemdState{
					Enabled: true,
					Started: true,
				},
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"systemctl is-enabled test.service": {
					stdout: "enabled",
				},
				"systemctl daemon-reload": {
					stdout: "",
					stderr: "",
				},
				"systemctl disable test.service": {
					stdout: "",
					stderr: "",
				},
			},
			wantOutput: SystemdOutput{
				PreviousState: SystemdState{
					Enabled: true,
					Started: true,
				},
				CurrentState: SystemdState{
					Enabled: false,
					Started: false,
				},
			},
		},
		{
			name: "revert with daemon reload failure",
			input: SystemdInput{
				Name:         "test.service",
				State:        "started",
				Enabled:      true,
				DaemonReload: true,
			},
			previous: SystemdOutput{
				PreviousState: SystemdState{
					Enabled: false,
					Started: false,
				},
			},
			mockOutput: map[string]struct {
				stdout string
				stderr string
				err    error
			}{
				"systemctl is-enabled test.service": {
					stdout: "enabled",
				},
				"systemctl daemon-reload": {
					stderr: "Failed to reload daemon",
					err:    fmt.Errorf("daemon reload failed"),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := newMockHostContext()

			module := SystemdModule{}
			output, err := module.Revert(tt.input, mockCtx, tt.previous, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			systemdOutput := output.(SystemdOutput)
			assert.Equal(t, tt.wantOutput.PreviousState, systemdOutput.PreviousState)
			assert.Equal(t, tt.wantOutput.CurrentState, systemdOutput.CurrentState)
		})
	}
}

func TestSystemdModule_ValidateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   SystemdInput
		wantErr bool
	}{
		{
			name: "valid input with all fields",
			input: SystemdInput{
				Name:         "test.service",
				State:        "started",
				Enabled:      true,
				DaemonReload: true,
			},
			wantErr: false,
		},
		{
			name: "missing name",
			input: SystemdInput{
				State:        "started",
				Enabled:      true,
				DaemonReload: true,
			},
			wantErr: true,
		},
		{
			name: "missing state",
			input: SystemdInput{
				Name:         "test.service",
				Enabled:      true,
				DaemonReload: true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSystemdState_Equal(t *testing.T) {
	tests := []struct {
		name      string
		state1    SystemdState
		state2    SystemdState
		wantEqual bool
	}{
		{
			name: "equal states",
			state1: SystemdState{
				Enabled: true,
				Started: true,
			},
			state2: SystemdState{
				Enabled: true,
				Started: true,
			},
			wantEqual: true,
		},
		{
			name: "different enabled state",
			state1: SystemdState{
				Enabled: true,
				Started: true,
			},
			state2: SystemdState{
				Enabled: false,
				Started: true,
			},
			wantEqual: false,
		},
		{
			name: "different started state",
			state1: SystemdState{
				Enabled: true,
				Started: true,
			},
			state2: SystemdState{
				Enabled: true,
				Started: false,
			},
			wantEqual: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			equal := tt.state1.Equal(tt.state2)
			assert.Equal(t, tt.wantEqual, equal)
		})
	}
}
