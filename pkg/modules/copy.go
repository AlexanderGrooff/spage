package modules

import (
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg/common"

	"github.com/AlexanderGrooff/spage/pkg"
)

type CopyModule struct{}

func (cm CopyModule) InputType() reflect.Type {
	return reflect.TypeOf(CopyInput{})
}

func (cm CopyModule) OutputType() reflect.Type {
	return reflect.TypeOf(CopyOutput{})
}

// Doc returns module-level documentation rendered into Markdown.
func (cm CopyModule) Doc() string {
	return `Copy files from the local machine to the target host. You can copy files from a source path or provide content directly.

## Examples

` + "```yaml" + `
- name: Copy a file
  copy:
    src: /path/to/local/file.txt
    dest: /path/to/remote/file.txt
    mode: '0644'

- name: Copy with inline content
  copy:
    content: |
      Hello World
      This is file content
    dest: /tmp/hello.txt
    mode: '0644'

- name: Copy from role files directory
  copy:
    src: config.conf
    dest: /etc/myapp/config.conf
` + "```" + `

**Note**: When used in roles, the copy module will first look for files in the role's ` + "`files/`" + ` directory before checking other locations.
`
}

// ParameterDocs provides rich documentation for copy module inputs.
func (cm CopyModule) ParameterDocs() map[string]pkg.ParameterDoc {
	notRequired := false
	required := true
	return map[string]pkg.ParameterDoc{
		"src": {
			Description: "Path to the source file on the local machine. Cannot be used with 'content'.",
			Required:    &notRequired, // optional - can use content instead
			Default:     "",
		},
		"content": {
			Description: "Content to write directly to the destination file. Cannot be used with 'src'.",
			Required:    &notRequired, // optional - can use src instead
			Default:     "",
		},
		"dest": {
			Description: "Path to the destination file on the target host.",
			Required:    &required, // dest is required
			Default:     "",
		},
		"mode": {
			Description: "File permissions to set on the destination file (e.g., '0644', '0755').",
			Required:    &notRequired, // mode is optional
			Default:     "",
		},
	}
}

type CopyInput struct {
	Content string `yaml:"content"`
	Src     string `yaml:"src"`
	Dst     string `yaml:"dest"`
	Mode    string `yaml:"mode"`
}

type CopyOutput struct {
	Contents pkg.RevertableChange[string]
	Mode     pkg.RevertableChange[string]
	pkg.ModuleOutput
}

func (i CopyInput) ToCode() string {
	return fmt.Sprintf("modules.CopyInput{Content: %q, Src: %q, Dst: %q, Mode: %q}",
		i.Content,
		i.Src,
		i.Dst,
		i.Mode,
	)
}

func (i CopyInput) GetVariableUsage() []string {
	vars := pkg.GetVariableUsageFromTemplate(i.Content)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Src)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Dst)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Mode)...)
	return vars
}

func (i CopyInput) ProvidesVariables() []string {
	return nil
}

// HasRevert indicates that the copy module can be reverted.
func (i CopyInput) HasRevert() bool {
	return true
}

func (i CopyInput) Validate() error {
	if i.Content == "" && i.Src == "" {
		return fmt.Errorf("missing Content or Src input")
	}
	if i.Dst == "" {
		return fmt.Errorf("missing Dst input")
	}
	return nil
}

func (o CopyOutput) String() string {
	return fmt.Sprintf("  original contents: %q, mode: %q\n  new contents: %q, mode: %q",
		o.Contents.Before, o.Mode.Before,
		o.Contents.After, o.Mode.After)
}

func (o CopyOutput) Changed() bool {
	return o.Contents.Changed() || o.Mode.Changed()
}

// readRoleAwareFile reads a file, checking role-specific paths first if the task is from a role
func (m CopyModule) readRoleAwareFile(filename string, closure *pkg.Closure) (string, error) {
	// If this is a role task, check role-specific files directory first
	if rolePath, ok := closure.GetFact("_spage_role_path"); ok && rolePath != "" {
		if rolePathStr, ok := rolePath.(string); ok {
			roleFilePath := filepath.Join(rolePathStr, "files", filename)
			if content, err := pkg.ReadLocalFile(roleFilePath); err == nil {
				return content, nil
			}
		}
	}

	// Fallback to default file resolution
	return pkg.ReadLocalFile(filename)
}

func (m CopyModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	copyParams, ok := params.(CopyInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected CopyInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected CopyInput, got %T", params)
	}

	if err := copyParams.Validate(); err != nil {
		return nil, err
	}

	// Get original state
	// Ignore error because dst is allowed to not exist
	var err error
	originalContents, _ := closure.HostContext.ReadFile(copyParams.Dst, runAs)
	originalMode := ""
	newContents := ""
	// Get mode using ls command if file exists
	if originalContents != "" {
		// TODO: get mode with Golang if local
		_, stdout, _, err := closure.HostContext.RunCommand(fmt.Sprintf("ls -l %s | cut -d ' ' -f 1", copyParams.Dst), runAs)
		if err == nil {
			originalMode = stdout[1:4] // Extract numeric mode from ls output
		}
	}

	checkMode := closure.IsCheckMode()

	if copyParams.Src != "" {
		// TODO: copy as user
		common.DebugOutput("Copying %s to %s", copyParams.Src, copyParams.Dst)

		// First, read contents using role-aware file resolution
		if newContents, err = m.readRoleAwareFile(copyParams.Src, closure); err != nil {
			return nil, fmt.Errorf("failed to read source file %s: %v", copyParams.Src, err)
		}

		// Write the contents to destination (skip in check mode)
		if checkMode {
			common.LogDebug("Would copy file from %s to %s", map[string]interface{}{
				"host": closure.HostContext.Host.Name,
				"src":  copyParams.Src,
				"dst":  copyParams.Dst,
			})
		} else {
			if err := closure.HostContext.WriteFile(copyParams.Dst, newContents, runAs); err != nil {
				return nil, fmt.Errorf("failed to write to %s: %v", copyParams.Dst, err)
			}
		}
	}

	if copyParams.Content != "" {
		if checkMode {
			common.LogDebug("Would write content to %s", map[string]interface{}{
				"host": closure.HostContext.Host.Name,
				"dst":  copyParams.Dst,
			})
		} else {
			if err := closure.HostContext.WriteFile(copyParams.Dst, copyParams.Content, runAs); err != nil {
				return nil, fmt.Errorf("failed to place contents in %s: %v", copyParams.Dst, err)
			}
		}
		newContents = copyParams.Content
	}

	// Apply mode if specified
	newMode := originalMode
	if copyParams.Mode != "" {
		if checkMode {
			common.LogDebug("Would set mode %s on %s", map[string]interface{}{
				"host": closure.HostContext.Host.Name,
				"mode": copyParams.Mode,
				"path": copyParams.Dst,
			})
		} else {
			if err := closure.HostContext.SetFileMode(copyParams.Dst, copyParams.Mode, runAs); err != nil {
				return nil, fmt.Errorf("failed to set mode %s on %s: %w", copyParams.Mode, copyParams.Dst, err)
			}
		}
		newMode = copyParams.Mode
	}

	return CopyOutput{
		Contents: pkg.RevertableChange[string]{
			Before: originalContents,
			After:  newContents,
		},
		Mode: pkg.RevertableChange[string]{
			Before: originalMode,
			After:  newMode,
		},
	}, nil
}

func (m CopyModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	copyParams, ok := params.(CopyInput)
	if !ok {
		if params == nil {
			// If params are nil, and revert is defined by Dest, we might not be able to proceed.
			// However, CopyInput.HasRevert() relies on Dest being set.
			// It's safer to error out if we expect CopyInput but get nil.
			return nil, fmt.Errorf("Revert: params is nil, expected CopyInput but got nil")
		}
		return nil, fmt.Errorf("Revert: incorrect parameter type: expected CopyInput, got %T", params)
	}

	if previous == nil {
		common.DebugOutput("Not reverting because previous result was nil")
		return CopyOutput{}, nil
	}

	prev := previous.(CopyOutput)
	if !prev.Changed() {
		return CopyOutput{}, nil
	}

	// Revert content
	if err := closure.HostContext.WriteFile(copyParams.Dst, prev.Contents.Before, runAs); err != nil {
		return nil, fmt.Errorf("failed to revert contents of %s: %v", copyParams.Dst, err)
	}

	// Revert mode
	if prev.Mode.Before != "" {
		if err := closure.HostContext.SetFileMode(copyParams.Dst, prev.Mode.Before, runAs); err != nil {
			return nil, fmt.Errorf("failed to revert mode on %s to %s: %w", copyParams.Dst, prev.Mode.Before, err)
		}
	}

	// Flip the before and after values
	return CopyOutput{
		Contents: pkg.RevertableChange[string]{
			Before: prev.Contents.After,
			After:  prev.Contents.Before,
		},
		Mode: pkg.RevertableChange[string]{
			Before: prev.Mode.After,
			After:  prev.Mode.Before,
		},
	}, nil
}

func init() {
	pkg.RegisterModule("copy", CopyModule{})
	pkg.RegisterModule("ansible.builtin.copy", CopyModule{})
}

// ParameterAliases defines aliases for module parameters.
func (cm CopyModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
