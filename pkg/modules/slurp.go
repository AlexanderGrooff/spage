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

// Doc returns module-level documentation rendered into Markdown.
func (m SlurpModule) Doc() string {
	return `Fetch a file from the remote host and encode its content as base64. This module is useful for reading configuration files or other data from remote systems.

## Examples

` + "```yaml" + `
- name: Read remote file
  slurp:
    src: /etc/hostname
  register: hostname_content

- name: Display file content
  debug:
    msg: "{{ hostname_content.content | b64decode }}"

- name: Read configuration file
  slurp:
    src: /etc/nginx/nginx.conf
  register: nginx_config

- name: Save remote file locally
  copy:
    content: "{{ remote_file.content | b64decode }}"
    dest: /tmp/local_copy.txt
  vars:
    remote_file: "{{ slurp_result }}"
` + "```" + `

**Note**: The file content is automatically base64 encoded. Use the 'b64decode' filter to get the original content.
`
}

// ParameterDocs provides rich documentation for slurp module inputs.
func (m SlurpModule) ParameterDocs() map[string]pkg.ParameterDoc {
	notRequired := false
	return map[string]pkg.ParameterDoc{
		"src": {
			Description: "Path to the file to fetch from the remote node. The file content will be base64 encoded.",
			Required:    &notRequired,
			Default:     "",
		},
	}
}

// SlurpInput defines the parameters for the slurp module.
type SlurpInput struct {
	Source string `yaml:"src"` // Path to the file to fetch from the remote node
}

// SlurpOutput holds the result of the slurp operation.
type SlurpOutput struct {
	Content      string `json:"content"`  // Base64 encoded content of the file
	Source       string `json:"source"`   // The source path provided
	Encoding     string `json:"encoding"` // The encoding of the content (always base64)
	ChangedState bool   `json:"changed"`  // Indicates whether the module changed the system state
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

func (i SlurpInput) ProvidesVariables() []string {
	return nil
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
	return o.ChangedState // This field will be false for slurp
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
func (m SlurpModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	slurpParams, ok := params.(SlurpInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected SlurpInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected SlurpInput, got %T", params)
	}

	if err := slurpParams.Validate(); err != nil {
		return nil, err
	}

	templatedSrc, err := pkg.TemplateString(slurpParams.Source, closure)
	if err != nil {
		return nil, fmt.Errorf("failed to template source path %q: %w", slurpParams.Source, err)
	}

	common.LogDebug("Attempting to slurp file", map[string]interface{}{"host": closure.HostContext.Host.Name, "source": templatedSrc})

	fileBytes, err := closure.HostContext.ReadFileBytes(templatedSrc, runAs)
	if err != nil {
		// Check if the error indicates the file was not found
		// The error message comes from runtime.Read*FileBytes now
		if strings.Contains(err.Error(), "file not found") {
			common.LogWarn("Slurp source file not found", map[string]interface{}{ // Keep warning for visibility
				"host":   closure.HostContext.Host.Name,
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
		Content:      encodedContent,
		Source:       templatedSrc,
		Encoding:     "base64",
		ChangedState: false, // Slurp itself doesn't change state
	}

	common.LogInfo("Slurp successful", map[string]interface{}{ // Log success
		"host":        closure.HostContext.Host.Name,
		"source":      output.Source,
		"bytes_read":  len(fileBytes),
		"encoded_len": len(output.Content),
	})
	return output, nil
}

// Revert for slurp is a no-op as it only reads information.
func (m SlurpModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	common.LogDebug("Revert called for slurp module (no-op)", map[string]interface{}{})
	if previous != nil {
		return previous, nil
	}
	// If no previous output, return a new SlurpOutput indicating no change.
	return SlurpOutput{Content: "", Encoding: "base64", Source: "", ChangedState: false}, nil
}

// ParameterAliases defines aliases if needed.
func (m SlurpModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for slurp
}

func init() {
	pkg.RegisterModule("slurp", SlurpModule{})
	pkg.RegisterModule("ansible.builtin.slurp", SlurpModule{})
}
