package modules

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/common"

	"github.com/AlexanderGrooff/spage/pkg"
)

type FileModule struct{}

func (fm FileModule) InputType() reflect.Type {
	return reflect.TypeOf(FileInput{})
}

func (fm FileModule) OutputType() reflect.Type {
	return reflect.TypeOf(FileOutput{})
}

type FileInput struct {
	Path  string `yaml:"path"` // Destination path
	State string `yaml:"state"`
	Mode  string `yaml:"mode"`
	Src   string `yaml:"src,omitempty"` // Source path for state=link
}

type FileOutput struct {
	State      pkg.RevertableChange[string] // "file", "directory", "link", "absent"
	Mode       pkg.RevertableChange[string] // Octal mode string
	Exists     pkg.RevertableChange[bool]
	IsLnk      pkg.RevertableChange[bool]   // Whether the path is a symlink
	LinkTarget pkg.RevertableChange[string] // Target of the symlink, if IsLnk is true
	pkg.ModuleOutput
}

func (i FileInput) ToCode() string {
	return fmt.Sprintf("modules.FileInput{Path: %q, State: %q, Mode: %q, Src: %q}",
		i.Path,
		i.State,
		i.Mode,
		i.Src,
	)
}

func (i FileInput) GetVariableUsage() []string {
	vars := pkg.GetVariableUsageFromTemplate(i.Path)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.State)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Mode)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Src)...)
	return vars
}

func (i FileInput) Validate() error {
	if i.Path == "" {
		return fmt.Errorf("missing Path input")
	}
	// Validate state values (basic validation before templating)
	validStates := map[string]bool{
		"touch":     true,
		"directory": true,
		"absent":    true,
		"link":      true,
		"file":      true, // Allow 'file' as alias for 'touch'
		"":          true, // Allow empty state (implies touch if path doesn't exist, or no state change if it does)
	}
	if !validStates[i.State] {
		return fmt.Errorf("invalid State value: %q (must be one of: touch, directory, absent, link, file)", i.State)
	}
	if i.State == "link" && i.Src == "" {
		return fmt.Errorf("missing Src input for state=link")
	}
	if i.State != "link" && i.Src != "" {
		return fmt.Errorf("src parameter is only valid for state=link")
	}

	return nil
}

func (i FileInput) HasRevert() bool {
	return true
}

func (i FileInput) ProvidesVariables() []string {
	return nil
}

func (o FileOutput) String() string {
	parts := []string{}
	if o.Exists.Changed() {
		if o.Exists.After {
			parts = append(parts, fmt.Sprintf("created %s", o.State.After))
		} else {
			parts = append(parts, fmt.Sprintf("removed %s", o.State.Before))
		}
	}
	if o.State.Changed() && !o.Exists.Changed() { // Don't report state change if existence changed
		parts = append(parts, fmt.Sprintf("state changed from %s to %s", o.State.Before, o.State.After))
	}
	if o.Mode.Changed() {
		parts = append(parts, fmt.Sprintf("mode changed from %q to %q", o.Mode.Before, o.Mode.After))
	}
	if o.IsLnk.Changed() {
		if o.IsLnk.After {
			parts = append(parts, fmt.Sprintf("is now link to %q", o.LinkTarget.After))
		} else {
			parts = append(parts, "is no longer link")
		}
	}
	if o.LinkTarget.Changed() && !o.IsLnk.Changed() && o.IsLnk.After {
		parts = append(parts, fmt.Sprintf("link target changed from %q to %q", o.LinkTarget.Before, o.LinkTarget.After))
	}

	if len(parts) == 0 {
		return "no changes made"
	}
	return strings.Join(parts, ", ")
}

func (o FileOutput) Changed() bool {
	return o.State.Changed() || o.Mode.Changed() || o.Exists.Changed() || o.IsLnk.Changed() || o.LinkTarget.Changed()
}

func getOriginalState(path string, c *pkg.HostContext) (exists bool, state, mode, linkTarget string, isLink bool, err error) {
	// Use c.Stat (Lstat) to determine type and mode of the path itself
	fileInfo, statErr := c.Stat(path, false) // follow=false to stat the link/file itself

	if statErr != nil {
		if os.IsNotExist(statErr) {
			// File doesn't exist, which is a valid state, not an error itself.
			common.DebugOutput("Path %s does not exist.", path)
			return false, "absent", "", "", false, nil // Return nil error
		} else {
			// Other error (permissions, etc.) - this IS an error.
			return false, "", "", "", false, fmt.Errorf("failed to stat %s: %w", path, statErr)
		}
	}

	// File exists, proceed to determine its state.
	exists = true
	fileMode := fileInfo.Mode()
	mode = fmt.Sprintf("0%o", fileMode.Perm()) // Get octal mode string

	// Determine file state based on mode
	if fileMode.IsDir() {
		state = "directory"
		isLink = false
	} else if fileMode&os.ModeSymlink != 0 {
		state = "link"
		isLink = true
		// Get link target using readlink command (os.Readlink or sftp ReadLink might be alternatives)
		// Need to run this command potentially as a different user.
		// Find the intended runAs user from the task parameters if available (TODO: Requires plumbing runAs to getOriginalState somehow or handling it in Execute)
		tempRunAs := ""                                                    // Placeholder - How to get the correct runAs for the readlink command?
		readlinkCmd := fmt.Sprintf("readlink %s", fmt.Sprintf("%q", path)) // Quote path
		_, targetStdout, targetStderr, targetErr := c.RunCommand(readlinkCmd, tempRunAs)
		if targetErr != nil {
			common.DebugOutput("WARNING: Failed to read link target for %s: %v, stderr: %s", path, targetErr, targetStderr)
			linkTarget = "" // Treat as broken link or unknown target
		} else {
			linkTarget = strings.TrimSpace(targetStdout)
		}
	} else if fileMode.IsRegular() {
		state = "file"
		isLink = false
	} else {
		// Treat other types (fifo, socket, char/block device) as 'file' for state management purposes?
		// This matches the previous behavior but might need refinement.
		common.DebugOutput("Treating file mode %s as 'file' state for %s", fileMode.String(), path)
		state = "file"
		isLink = false
	}

	return exists, state, mode, linkTarget, isLink, nil
}

func (m FileModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	fileParams, ok := params.(FileInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected FileInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected FileInput, got %T", params)
	}

	if err := fileParams.Validate(); err != nil {
		return nil, err
	}

	templatedPath, err := pkg.TemplateString(fileParams.Path, closure)
	if err != nil {
		return nil, fmt.Errorf("failed to template path %s: %w", fileParams.Path, err)
	}

	templatedSrc, err := pkg.TemplateString(fileParams.Src, closure)
	if err != nil {
		return nil, fmt.Errorf("failed to template src %s: %w", fileParams.Src, err)
	}
	templatedMode, err := pkg.TemplateString(fileParams.Mode, closure)
	if err != nil {
		return nil, fmt.Errorf("failed to template mode %s: %w", fileParams.Mode, err)
	}
	templatedState, err := pkg.TemplateString(fileParams.State, closure)
	if err != nil {
		return nil, fmt.Errorf("failed to template state %s: %w", fileParams.State, err)
	}

	// 1. Get original state - runAs is needed later for commands like rm, readlink
	originalExists, originalState, originalMode, originalLinkTarget, originalIsLnk, err := getOriginalState(templatedPath, closure.HostContext) // Removed runAs from call
	if err != nil {
		return nil, err // Propagate error from getOriginalState
	}

	// Prepare to track changes
	newState := originalState
	newMode := originalMode
	newIsLnk := originalIsLnk
	newLinkTarget := originalLinkTarget
	newExists := originalExists
	actionTaken := false

	// Determine desired state (treat touch/file as 'file')
	desiredState := templatedState
	if desiredState == "touch" || desiredState == "file" {
		desiredState = "file"
	}
	if desiredState == "" {
		if !originalExists {
			desiredState = "file" // Default to creating a file if state is empty and path doesn't exist
		} else {
			desiredState = originalState // No state change requested, keep original
		}
	}

	// Check mode detection
	checkMode := closure.IsCheckMode()

	// 2. Apply state changes if needed
	if desiredState != originalState || !originalExists {
		common.DebugOutput("Applying state change from %q to %q for %s", originalState, desiredState, templatedPath)
		actionTaken = true
		if checkMode {
			common.LogDebug("Would change %s from %s to %s", map[string]interface{}{
				"host": closure.HostContext.Host.Name,
				"path": templatedPath,
				"from": originalState,
				"to":   desiredState,
			})
			// Simulate outcome without changing system
			switch desiredState {
			case "file":
				newExists = true
				newState = "file"
			case "directory":
				newExists = true
				newState = "directory"
			case "absent":
				newExists = false
				newState = "absent"
			case "link":
				newExists = true
				newState = "link"
				newLinkTarget = templatedSrc
			}
		} else {
			// If target state is different from absent, ensure path is clear first unless creating a directory
			if originalExists && desiredState != originalState && desiredState != "absent" && desiredState != "directory" {
				if _, _, _, err := closure.HostContext.RunCommand(fmt.Sprintf("rm -rf %s", fmt.Sprintf("%q", templatedPath)), runAs); err != nil {
					return nil, fmt.Errorf("failed to remove existing path %s before changing state: %v", templatedPath, err)
				}
			}

			switch desiredState {
			case "file":
				// Ensure path exists as a file (touch)
				if _, _, _, err := closure.HostContext.RunCommand(fmt.Sprintf("touch %s", fmt.Sprintf("%q", templatedPath)), runAs); err != nil {
					return nil, fmt.Errorf("failed to touch file %s: %v", templatedPath, err)
				}
				newExists = true
				newState = "file"
			case "directory":
				if _, _, _, err := closure.HostContext.RunCommand(fmt.Sprintf("mkdir -p %s", fmt.Sprintf("%q", templatedPath)), runAs); err != nil {
					return nil, fmt.Errorf("failed to create directory %s: %v", templatedPath, err)
				}
				newExists = true
				newState = "directory"
			case "absent":
				if originalExists {
					if _, _, _, err := closure.HostContext.RunCommand(fmt.Sprintf("rm -rf %s", fmt.Sprintf("%q", templatedPath)), runAs); err != nil {
						return nil, fmt.Errorf("failed to remove %s: %v", templatedPath, err)
					}
				}
				newExists = false
				newState = "absent"
			case "link":
				// Remove existing path if it's not already the link we want to create/update
				if originalExists && (originalState != "link" || (originalState == "link" && originalLinkTarget != templatedSrc)) {
					if _, _, _, err := closure.HostContext.RunCommand(fmt.Sprintf("rm -f %s", fmt.Sprintf("%q", templatedPath)), runAs); err != nil {
						return nil, fmt.Errorf("failed to remove existing file before creating link %s: %v", templatedPath, err)
					}
				}
				// Create the symlink
				if _, _, _, err := closure.HostContext.RunCommand(fmt.Sprintf("ln -sfn %s %s", fmt.Sprintf("%q", templatedSrc), fmt.Sprintf("%q", templatedPath)), runAs); err != nil {
					return nil, fmt.Errorf("failed to create symlink from %s to %s: %v", templatedSrc, templatedPath, err)
				}
				newExists = true
				newState = "link"
			}
		}
	} else if desiredState == "link" && originalLinkTarget != templatedSrc {
		// Special case: State is already 'link', but the target needs updating
		common.DebugOutput("Updating link target for %s from %q to %q", templatedPath, originalLinkTarget, templatedSrc)
		actionTaken = true
		if checkMode {
			common.LogDebug("Would update link target for %s from %s to %s", map[string]interface{}{
				"host": closure.HostContext.Host.Name,
				"path": templatedPath,
				"from": originalLinkTarget,
				"to":   templatedSrc,
			})
			newLinkTarget = templatedSrc
		} else {
			// Recreate the link with the new target
			linkCmd := fmt.Sprintf("ln -sf %s %s", fmt.Sprintf("%q", templatedSrc), fmt.Sprintf("%q", templatedPath)) // Quote src and path
			if _, _, _, err := closure.HostContext.RunCommand(linkCmd, runAs); err != nil {
				return nil, fmt.Errorf("failed to update link %s -> %s: %v", templatedPath, templatedSrc, err)
			}
			newLinkTarget = templatedSrc // Only target changes
		}
	}

	// 3. Apply mode changes if specified AND the file/dir exists after state changes
	if templatedMode != "" && newExists && newState != "link" { // Don't apply mode directly to links
		// Convert symbolic mode (e.g., u+x, g-w) or octal string to final mode
		// For now, we only handle simple 3-digit octal modes like Ansible's basic usage
		// TODO: Implement more complex mode handling if needed (like Ansible does)
		finalMode := templatedMode                    // Assume templatedMode is octal for now
		if finalMode != originalMode || actionTaken { // Apply if mode differs or if file was just created/state changed
			common.DebugOutput("Applying mode %s to %s", finalMode, templatedPath)
			if checkMode {
				common.LogDebug("Would set mode %s on %s", map[string]interface{}{
					"host": closure.HostContext.Host.Name,
					"mode": finalMode,
					"path": templatedPath,
				})
				newMode = finalMode
			} else {
				if err := closure.HostContext.SetFileMode(templatedPath, finalMode, runAs); err != nil {
					// Attempting to chmod a link target might fail if the link is broken
					// SetFileMode handles local/remote automatically
					// It might still fail for links, keep the warning
					if newState == "link" {
						common.DebugOutput("WARNING: Failed to set mode on target of link %s (mode %s): %v. This might be expected if link is broken.", templatedPath, finalMode, err)
					} else {
						return nil, fmt.Errorf("failed to set mode %s on %s: %w", finalMode, templatedPath, err)
					}
				} else {
					newMode = finalMode
				}
			}
		}
	}

	// 4. Populate output
	out := FileOutput{
		State: pkg.RevertableChange[string]{
			Before: originalState,
			After:  newState,
		},
		Mode: pkg.RevertableChange[string]{
			Before: originalMode,
			After:  newMode,
		},
		Exists: pkg.RevertableChange[bool]{
			Before: originalExists,
			After:  newExists,
		},
		IsLnk: pkg.RevertableChange[bool]{
			Before: originalIsLnk,
			After:  newIsLnk,
		},
		LinkTarget: pkg.RevertableChange[string]{
			Before: originalLinkTarget,
			After:  newLinkTarget,
		},
	}

	return out, nil
}

func (m FileModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	fileParams, ok := params.(FileInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Revert: params is nil, expected FileInput but got nil")
		}
		return nil, fmt.Errorf("Revert: incorrect parameter type: expected FileInput, got %T", params)
	}

	templatedPath, err := pkg.TemplateString(fileParams.Path, closure)
	if err != nil {
		return nil, fmt.Errorf("failed to template path %s for revert: %w", fileParams.Path, err)
	}

	if previous == nil {
		common.DebugOutput("Not reverting file module because previous result was nil")
		return FileOutput{}, nil
	}

	prev := previous.(FileOutput)
	if !prev.Changed() {
		common.DebugOutput("Not reverting file module because no changes were made")
		return FileOutput{}, nil
	}

	common.DebugOutput("Reverting file state for %s from [Exists:%t State:%s Mode:%s Link:%t Target:%s] to [Exists:%t State:%s Mode:%s Link:%t Target:%s]",
		templatedPath, prev.Exists.After, prev.State.After, prev.Mode.After, prev.IsLnk.After, prev.LinkTarget.After,
		prev.Exists.Before, prev.State.Before, prev.Mode.Before, prev.IsLnk.Before, prev.LinkTarget.Before)

	// Revert to original existence and state
	if !prev.Exists.Before {
		// Original state was absent, so remove whatever is there now
		common.DebugOutput("Reverting to absent: removing %s", templatedPath)
		if _, _, _, err := closure.HostContext.RunCommand(fmt.Sprintf("rm -rf %s", fmt.Sprintf("%q", templatedPath)), runAs); err != nil {
			return nil, fmt.Errorf("revert failed: could not remove %s: %v", templatedPath, err)
		}
	} else {
		// Original state existed, ensure it exists now in the correct state
		// Remove current path first to handle state changes (e.g., dir -> file)
		common.DebugOutput("Removing current path %s before reverting to original state %s", templatedPath, prev.State.Before)
		if _, _, _, err := closure.HostContext.RunCommand(fmt.Sprintf("rm -rf %s", fmt.Sprintf("%q", templatedPath)), runAs); err != nil {
			common.DebugOutput("Warning during revert: failed to remove existing path %s (might be ok if state didn't change drastically): %v", templatedPath, err)
		}

		switch prev.State.Before {
		case "file":
			common.DebugOutput("Reverting to file: touching %s", templatedPath)
			if _, _, _, err := closure.HostContext.RunCommand(fmt.Sprintf("touch %s", fmt.Sprintf("%q", templatedPath)), runAs); err != nil {
				return nil, fmt.Errorf("revert failed: could not touch file %s: %v", templatedPath, err)
			}
		case "directory":
			common.DebugOutput("Reverting to directory: mkdir -p %s", templatedPath)
			if _, _, _, err := closure.HostContext.RunCommand(fmt.Sprintf("mkdir -p %s", fmt.Sprintf("%q", templatedPath)), runAs); err != nil {
				return nil, fmt.Errorf("revert failed: could not create directory %s: %v", templatedPath, err)
			}
		case "link":
			if prev.LinkTarget.Before == "" {
				return nil, fmt.Errorf("revert failed: cannot revert link for %s, original target unknown", templatedPath)
			}
			common.DebugOutput("Reverting to link: ln -sf %s %s", prev.LinkTarget.Before, templatedPath)
			linkCmd := fmt.Sprintf("ln -sf %s %s", fmt.Sprintf("%q", prev.LinkTarget.Before), fmt.Sprintf("%q", templatedPath)) // Quote paths
			if _, _, _, err := closure.HostContext.RunCommand(linkCmd, runAs); err != nil {
				return nil, fmt.Errorf("revert failed: could not create link %s -> %s: %v", templatedPath, prev.LinkTarget.Before, err)
			}
		case "absent":
			common.DebugOutput("Revert: Original state was absent, already handled.")
		default:
			return nil, fmt.Errorf("revert failed: unknown original state %q for %s", prev.State.Before, templatedPath)
		}

		// Revert mode *after* recreating the correct state, but *only* if the original state was not a link
		if prev.Mode.Before != "" && !prev.IsLnk.Before {
			common.DebugOutput("Reverting mode to %s for %s", prev.Mode.Before, templatedPath)
			if err := closure.HostContext.SetFileMode(templatedPath, prev.Mode.Before, runAs); err != nil {
				return nil, fmt.Errorf("revert failed: could not set mode %s on %s: %w", prev.Mode.Before, templatedPath, err)
			}
		}
	}

	// Flip the before and after values for the output
	return FileOutput{
		State: pkg.RevertableChange[string]{
			Before: prev.State.After,
			After:  prev.State.Before,
		},
		Mode: pkg.RevertableChange[string]{
			Before: prev.Mode.After,
			After:  prev.Mode.Before,
		},
		Exists: pkg.RevertableChange[bool]{
			Before: prev.Exists.After,
			After:  prev.Exists.Before,
		},
		IsLnk: pkg.RevertableChange[bool]{
			Before: prev.IsLnk.After,
			After:  prev.IsLnk.Before,
		},
		LinkTarget: pkg.RevertableChange[string]{
			Before: prev.LinkTarget.After,
			After:  prev.LinkTarget.Before,
		},
	}, nil
}

func init() {
	pkg.RegisterModule("file", FileModule{})
	pkg.RegisterModule("ansible.builtin.file", FileModule{})
}

// ParameterAliases defines aliases for the file module parameters.
func (fm FileModule) ParameterAliases() map[string]string {
	return map[string]string{
		"dest": "path",
		"name": "path",
	}
}
