package modules

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"gopkg.in/yaml.v3"
)

// SlurpModule fetches a file from the remote host and stores its content.
type SlurpModule struct{}

func (m SlurpModule) InputType() reflect.Type {
	return reflect.TypeOf(SlurpInput{})
}

func (m SlurpModule) OutputType() reflect.Type {
	return reflect.TypeOf(SlurpOutput{})
}

// SlurpInput defines the parameters for the slurp module.
type SlurpInput struct {
	Source string `yaml:"src"` // Path to the file to fetch from the remote node
	pkg.ModuleInput
}

// SlurpOutput holds the result of the slurp operation.
type SlurpOutput struct {
	Content  string `json:"content"`  // Base64 encoded content of the file
	Source   string `json:"source"`   // The source path provided
	Encoding string `json:"encoding"` // The encoding of the content (always base64)
	pkg.ModuleOutput
}

// ToCode generates the Go code representation of the SlurpInput.
func (i SlurpInput) ToCode() string {
	return fmt.Sprintf("modules.SlurpInput{Source: %q}", i.Source)
}

// GetVariableUsage extracts variables used within the source path.
func (i SlurpInput) GetVariableUsage() []string {
	return pkg.GetVariableUsageFromTemplate(i.Source)
}

// Validate checks if the required 'src' parameter is provided.
func (i SlurpInput) Validate() error {
	if i.Source == "" {
		return fmt.Errorf("missing required parameter 'src' for slurp module")
	}
	return nil
}

// HasRevert indicates that slurp does not have a revert action.
func (i SlurpInput) HasRevert() bool {
	return false
}

// String provides a human-readable summary of the SlurpOutput.
func (o SlurpOutput) String() string {
	// Truncate content for readability if it's long
	contentPreview := o.Content
	if len(contentPreview) > 50 {
		contentPreview = contentPreview[:47] + "..."
	}
	return fmt.Sprintf("Slurped file %q, content (base64): %s", o.Source, contentPreview)
}

// Changed indicates whether the module changed the system state. Slurp is read-only.
func (o SlurpOutput) Changed() bool {
	return false
}

// AsFacts provides the output data in a map suitable for registration.
func (o SlurpOutput) AsFacts() map[string]interface{} {
	return map[string]interface{}{
		"content":  o.Content,
		"source":   o.Source,
		"encoding": o.Encoding,
		"changed":  o.Changed(),
	}
}

// UnmarshalYAML handles both shorthand (string) and map inputs for the slurp module.
func (i *SlurpInput) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
		// Handle shorthand: slurp: /path/to/file
		i.Source = node.Value
		return nil
	}

	if node.Kind == yaml.MappingNode {
		// Handle standard map format: slurp: { src: ... }
		// Use a temporary type to avoid recursion
		type SlurpInputMap struct {
			Source string `yaml:"src"`
		}
		var tmp SlurpInputMap
		if err := node.Decode(&tmp); err != nil {
			return fmt.Errorf("failed to decode slurp input map: %w", err)
		}
		i.Source = tmp.Source
		return nil
	}

	return fmt.Errorf("invalid type for slurp module input: expected string or map, got %v", node.Tag)
}

// Execute runs the slurp operation on the remote host.
func (m SlurpModule) Execute(params pkg.ModuleInput, c *pkg.HostContext, runAs string) (pkg.ModuleOutput, error) {
	input, ok := params.(SlurpInput)
	if !ok {
		return nil, fmt.Errorf("invalid params type (%T) for slurp module", params)
	}

	if err := input.Validate(); err != nil {
		return nil, err
	}

	templatedSrc, err := pkg.TemplateString(input.Source, c.Facts)
	if err != nil {
		return nil, fmt.Errorf("failed to template source path %q: %w", input.Source, err)
	}

	common.LogDebug("Attempting to slurp file", map[string]interface{}{"host": c.Host.Name, "source": templatedSrc})

	fileBytes, err := c.ReadFileBytes(templatedSrc, runAs)
	if err != nil {
		// Check if the error indicates the file was not found
		// The error message comes from runtime.Read*FileBytes now
		if strings.Contains(err.Error(), "file not found") {
			common.LogWarn("Slurp source file not found", map[string]interface{}{ // Keep warning for visibility
				"host":   c.Host.Name,
				"source": templatedSrc,
				"error":  err.Error(), // Log the specific error
			})
			// Return a specific error message for playbook use
			return nil, fmt.Errorf("source file not found: %s", templatedSrc)
		}
		// Otherwise, return the general file reading error
		return nil, fmt.Errorf("failed to read file %q for slurp: %w", templatedSrc, err)
	}

	// Encode the bytes using Go's standard library
	encodedContent := base64.StdEncoding.EncodeToString(fileBytes)

	output := SlurpOutput{
		Content:  encodedContent,
		Source:   templatedSrc, // Use templated path in output
		Encoding: "base64",
	}

	common.LogInfo("Slurp successful", map[string]interface{}{ // Log success
		"host": c.Host.Name, "source": output.Source, "bytes_read": len(fileBytes), "encoded_len": len(output.Content),
	})
	return output, nil
}

// Revert is a no-op for the read-only slurp module.
func (m SlurpModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	common.LogDebug("Revert called for slurp module (no-op)")
	return SlurpOutput{}, nil // Return empty output, no state change
}

// ParameterAliases defines aliases if needed.
func (m SlurpModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for slurp
}

func init() {
	pkg.RegisterModule("slurp", SlurpModule{})
}
