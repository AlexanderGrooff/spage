package modules

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/AlexanderGrooff/spage-protobuf/spage/core"
	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
	"gopkg.in/yaml.v3"
)

type SpageModule struct{}

func (m SpageModule) InputType() reflect.Type {
	return reflect.TypeOf(SpageInput{})
}

func (m SpageModule) OutputType() reflect.Type {
	return reflect.TypeOf(SpageOutput{})
}

// Doc returns module-level documentation rendered into Markdown.
func (m SpageModule) Doc() string {
	return `Interact with the local Spage daemon over gRPC.

## Examples

` + "```yaml" + `
- name: Ping local daemon
  spage:
    action: ping
    grpc_endpoint: localhost:9091

- name: Start a play on a daemon
  spage:
    action: start
    grpc_endpoint: localhost:9091
    play_id: demo-001
    playbook: /path/to/playbook.yaml
    inventory: /path/to/inventory.yaml
    variables:
      hello: world

- name: Cancel a play
  spage:
    action: cancel
    grpc_endpoint: localhost:9091
    play_id: demo-001
    reason: user-request
` + "```" + `
`
}

// ParameterDocs provides documentation for inputs.
func (m SpageModule) ParameterDocs() map[string]pkg.ParameterDoc {
	notRequired := false
	required := true
	return map[string]pkg.ParameterDoc{
		"action": {
			Description: "Command to send: start | cancel | ping",
			Required:    &required,
			Default:     "",
		},
		"grpc_endpoint": {
			Description: "Daemon gRPC endpoint (host:port)",
			Required:    &notRequired,
			Default:     "localhost:9091",
		},
		"play_id": {
			Description: "Play ID for start/cancel. Will be generated if empty.",
			Required:    &notRequired,
			Default:     "",
		},
		"playbook": {
			Description: "Playbook path or bundle URI for start",
			Required:    &notRequired,
			Default:     "",
		},
		"inventory": {
			Description: "Inventory path for start",
			Required:    &notRequired,
			Default:     "",
		},
		"variables": {
			Description: "Variables map for start",
			Required:    &notRequired,
			Default:     "",
		},
		"reason": {
			Description: "Cancel reason",
			Required:    &notRequired,
			Default:     "",
		},
		"correlation_id": {
			Description: "Optional correlation ID; generated if empty",
			Required:    &notRequired,
			Default:     "",
		},
	}
}

type SpageInput struct {
	Action        string            `yaml:"action"`
	GRPCEndpoint  string            `yaml:"grpc_endpoint"`
	PlayID        string            `yaml:"play_id"`
	Playbook      string            `yaml:"playbook"`
	Inventory     string            `yaml:"inventory"`
	Variables     map[string]string `yaml:"variables"`
	Reason        string            `yaml:"reason"`
	CorrelationID string            `yaml:"correlation_id"`
}

type SpageOutput struct {
	Published     bool           `yaml:"published"`
	Subject       string         `yaml:"subject"`
	CorrelationID string         `yaml:"correlation_id"`
	Response      map[string]any `yaml:"response,omitempty"`
	pkg.ModuleOutput
}

func (i SpageInput) ToCode() string {
	return fmt.Sprintf("modules.SpageInput{Action: %q, GRPCEndpoint: %q, PlayID: %q, Playbook: %q, Inventory: %q, Variables: %#v, Reason: %q, CorrelationID: %q}",
		i.Action, i.GRPCEndpoint, i.PlayID, i.Playbook, i.Inventory, i.Variables, i.Reason, i.CorrelationID,
	)
}

func (i SpageInput) GetVariableUsage() []string {
	vars := []string{}
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Action)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.GRPCEndpoint)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.PlayID)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Playbook)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Inventory)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Reason)...)
	for _, v := range i.Variables {
		vars = append(vars, pkg.GetVariableUsageFromTemplate(v)...)
	}
	return vars
}

// HasRevert indicates whether this input defines a revert operation (it does not).
func (i SpageInput) HasRevert() bool { return false }

// ProvidesVariables returns variables this input defines (none for messaging).
func (i SpageInput) ProvidesVariables() []string { return nil }

func (i SpageInput) Validate() error {
	a := strings.ToLower(strings.TrimSpace(i.Action))
	if a == "" {
		return fmt.Errorf("action is required")
	}
	if i.PlayID == "" {
		i.PlayID = uuid.NewString()
	}
	switch a {
	case "ping":
		return nil
	case "start":
		if i.PlayID == "" || i.Playbook == "" {
			return fmt.Errorf("start requires play_id and playbook")
		}
		return nil
	case "cancel":
		if i.PlayID == "" {
			return fmt.Errorf("cancel requires play_id")
		}
		return nil
	default:
		return fmt.Errorf("unsupported action: %s", i.Action)
	}
}

func (o SpageOutput) String() string {
	if o.Response != nil {
		return fmt.Sprintf("published=%t subject=%q correlation_id=%q response=%v", o.Published, o.Subject, o.CorrelationID, o.Response)
	}
	return fmt.Sprintf("published=%t subject=%q correlation_id=%q", o.Published, o.Subject, o.CorrelationID)
}

func (o SpageOutput) Changed() bool {
	return true
}

// UnmarshalYAML implements flexible decoding for SpageInput (mapping only)
func (i *SpageInput) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("invalid type for spage module input: expected map, got %v", node.Tag)
	}
	type alias SpageInput
	var tmp alias
	if err := node.Decode(&tmp); err != nil {
		return fmt.Errorf("failed to decode spage input map: %w", err)
	}
	*i = SpageInput(tmp)
	return nil
}

func (m SpageModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	// Decode params
	in, ok := params.(SpageInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected SpageInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected SpageInput, got %T", params)
	}

	if closure.IsCheckMode() && in.Action != "ping" {
		return SpageOutput{}, nil
	}

	// Resolve endpoint
	endpoint := strings.TrimSpace(in.GRPCEndpoint)
	if endpoint == "" {
		// Backward-compatible default
		endpoint = "localhost:9091"
	}

	// Create gRPC client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc connect to %s: %w", endpoint, err)
	}
	defer func() { _ = conn.Close() }()

	client := core.NewSpageExecutionClient(conn)

	// CorrelationID maintained for output compatibility
	correlationID := strings.TrimSpace(in.CorrelationID)
	if correlationID == "" {
		correlationID = uuid.NewString()
	}

	action := strings.ToLower(strings.TrimSpace(in.Action))
	switch action {
	case "ping":
		hs, err := client.HealthCheck(ctx, &emptypb.Empty{})
		if err != nil {
			return nil, fmt.Errorf("grpc HealthCheck to %s: %w", endpoint, err)
		}
		return SpageOutput{Published: true, Subject: "grpc://" + endpoint + "/HealthCheck", CorrelationID: correlationID, Response: map[string]any{"status": hs.Status, "details": hs.Details}}, nil
	case "start":
		req := &core.ExecutePlayRequest{
			PlayId:    in.PlayID,
			Playbook:  in.Playbook,
			Inventory: in.Inventory,
			Variables: in.Variables,
		}
		resp, err := client.ExecutePlay(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("grpc ExecutePlay to %s: %w", endpoint, err)
		}
		if !resp.Success {
			if resp.Error != nil {
				return nil, fmt.Errorf("execute failed to %s: %s", endpoint, resp.Error.Message)
			}
			return nil, fmt.Errorf("execute failed to %s", endpoint)
		}
		return SpageOutput{Published: true, Subject: "grpc://" + endpoint + "/ExecutePlay", CorrelationID: correlationID, Response: map[string]any{"success": resp.Success, "play_id": resp.PlayId}}, nil
	case "cancel":
		req := &core.CancelPlayRequest{PlayId: in.PlayID, Reason: in.Reason}
		resp, err := client.CancelPlay(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("grpc CancelPlay to %s: %w", endpoint, err)
		}
		if !resp.Success {
			return nil, fmt.Errorf("cancel failed to %s", endpoint)
		}
		return SpageOutput{Published: true, Subject: "grpc://" + endpoint + "/CancelPlay", CorrelationID: correlationID, Response: map[string]any{"success": resp.Success}}, nil
	default:
		return nil, fmt.Errorf("unsupported action: %s", in.Action)
	}
}

func (m SpageModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	// No-op revert for messaging
	return SpageOutput{Published: false, Subject: "", CorrelationID: ""}, nil
}

// ParameterAliases keeps module param names stable; none for spage currently.
func (m SpageModule) ParameterAliases() map[string]string { return map[string]string{} }

func init() {
	pkg.RegisterModule("spage", SpageModule{})
}
