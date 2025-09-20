package modules

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
)

type LineinfileModule struct{}

func (lm LineinfileModule) InputType() reflect.Type {
	return reflect.TypeOf(LineinfileInput{})
}

func (lm LineinfileModule) OutputType() reflect.Type {
	return reflect.TypeOf(LineinfileOutput{})
}

// Doc returns module-level documentation rendered into Markdown.
func (lm LineinfileModule) Doc() string {
	return `Ensure a particular line is in a file, or replace an existing line using a back-referenced regular expression. This module is useful for making small changes to configuration files.

## Examples

` + "```yaml" + `
- name: Add a line to a file
  lineinfile:
    path: /etc/hosts
    line: "192.168.1.100 myhost.example.com"

- name: Replace a line using regex
  lineinfile:
    path: /etc/ssh/sshd_config
    regexp: '^#?PermitRootLogin'
    line: 'PermitRootLogin no'

- name: Remove a line
  lineinfile:
    path: /etc/hosts
    regexp: '^192\.168\.1\.100'
    state: absent

- name: Insert line after pattern
  lineinfile:
    path: /etc/fstab
    insertafter: '^/dev/sda1'
    line: '/dev/sda2 /home ext4 defaults 0 2'

- name: Create file if it doesn't exist
  lineinfile:
    path: /etc/myapp.conf
    line: 'setting=value'
    create: true
    mode: '0644'
` + "```" + `

**Note**: Use regular expressions carefully to avoid unintended matches. The module will create the file if it doesn't exist and create=true is specified.
`
}

// ParameterDocs provides rich documentation for lineinfile module inputs.
func (lm LineinfileModule) ParameterDocs() map[string]pkg.ParameterDoc {
	notRequired := false
	return map[string]pkg.ParameterDoc{
		"path": {
			Description: "Path to the file to modify. Aliases: dest, destfile, name.",
			Required:    &notRequired,
			Default:     "",
		},
		"line": {
			Description: "The line to insert or replace. Required when state=present.",
			Required:    &notRequired,
			Default:     "",
		},
		"regexp": {
			Description: "Regular expression to match existing lines for replacement or removal.",
			Required:    &notRequired,
			Default:     "",
		},
		"state": {
			Description: "Whether the line should be present or absent.",
			Required:    &notRequired,
			Default:     "present",
			Choices:     []string{"present", "absent"},
		},
		"backrefs": {
			Description: "Use backreferences from regexp in the line. Only works with regexp.",
			Required:    &notRequired,
			Default:     "false",
			Choices:     []string{"true", "false"},
		},
		"create": {
			Description: "Create the file if it doesn't exist.",
			Required:    &notRequired,
			Default:     "false",
			Choices:     []string{"true", "false"},
		},
		"insertafter": {
			Description: "Insert line after this regex pattern, or 'EOF' for end of file.",
			Required:    &notRequired,
			Default:     "",
		},
		"insertbefore": {
			Description: "Insert line before this regex pattern, or 'BOF' for beginning of file.",
			Required:    &notRequired,
			Default:     "",
		},
		"mode": {
			Description: "File permissions to set if creating the file (e.g., '0644').",
			Required:    &notRequired,
			Default:     "",
		},
	}
}

type LineinfileInput struct {
	Path         string `yaml:"path"` // Aliases: dest, destfile, name
	Line         string `yaml:"line"`
	Regexp       string `yaml:"regexp"`
	State        string `yaml:"state,omitempty"`        // "present" or "absent", defaults to "present"
	Backrefs     bool   `yaml:"backrefs,omitempty"`     // Use backreferences from regexp in line
	Create       bool   `yaml:"create,omitempty"`       // Create the file if it doesn't exist
	InsertAfter  string `yaml:"insertafter,omitempty"`  // Regex, or "EOF"
	InsertBefore string `yaml:"insertbefore,omitempty"` // Regex, or "BOF"
	Mode         string `yaml:"mode,omitempty"`         // File mode if created
}

// LineinfileOutput contains the result information for a lineinfile operation.
type LineinfileOutput struct {
	Msg                string                       `yaml:"msg"`
	Diff               pkg.RevertableChange[string] `yaml:"-"` // Don't serialize diff in YAML
	OriginalFileExists bool                         `yaml:"-"`
	OriginalMode       string                       `yaml:"-"`
	ShouldShowDiff     bool                         `yaml:"-"` // New field to control diff display
	pkg.ModuleOutput
}

// ToCode generates the Go code representation of the input.
func (i LineinfileInput) ToCode() string {
	return fmt.Sprintf("modules.LineinfileInput{Path: %q, Line: %q, Regexp: %q, State: %q, Backrefs: %t, Create: %t, InsertAfter: %q, InsertBefore: %q, Mode: %q}",
		i.Path, i.Line, i.Regexp, i.State, i.Backrefs, i.Create, i.InsertAfter, i.InsertBefore, i.Mode)
}

// GetVariableUsage identifies variables used in templates.
func (i LineinfileInput) GetVariableUsage() []string {
	var vars []string
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Path)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Line)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Regexp)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.InsertAfter)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.InsertBefore)...)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.Mode)...)
	return vars
}

// Validate checks the input parameters for correctness.
func (i LineinfileInput) Validate() error {
	if i.Path == "" {
		return fmt.Errorf("path parameter is required for lineinfile module")
	}
	if i.State == "" {
		// Default to "present" if not specified, handled in Execute
	} else if i.State != "present" && i.State != "absent" {
		return fmt.Errorf("state parameter must be 'present' or 'absent', got '%s'", i.State)
	}
	if i.Backrefs && i.Regexp == "" {
		return fmt.Errorf("backrefs=true requires regexp to be set")
	}
	if i.Line == "" && i.State == "present" && i.Regexp == "" {
		// This condition is tricky. Ansible's lineinfile allows ensuring a line *matching regexp* is present
		// without specifying `line` if `regexp` is used for matching.
		// For now, let's assume if state=present, `line` is usually needed unless `regexp` is the target.
		// If `line` is empty and `regexp` is set, it means "ensure a line matching regexp exists".
		// If `line` is also empty and `state` is `present` this is an issue.
		return fmt.Errorf("line parameter is required when state=present unless regexp is used to identify the target line")
	}
	if i.InsertAfter != "" && i.InsertBefore != "" {
		return fmt.Errorf("cannot specify both insertafter and insertbefore")
	}
	return nil
}

// HasRevert indicates that the lineinfile module can be reverted.
func (i LineinfileInput) HasRevert() bool {
	return true
}

// ProvidesVariables returns nil as lineinfile doesn't inherently define variables.
func (i LineinfileInput) ProvidesVariables() []string {
	return nil
}

// Changed indicates if the module made any changes.
func (o LineinfileOutput) Changed() bool {
	return o.Diff.Changed() || (o.Msg != "" && !strings.Contains(o.Msg, "already")) // A bit simplistic, refine
}

// String provides a human-readable summary of the output.
func (o LineinfileOutput) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  msg: %s\n", o.Msg))
	// Only show diff if ShouldShowDiff is true AND there's actually a change
	if o.ShouldShowDiff && o.Diff.Changed() {
		sb.WriteString("  diff: |\n")
		// Indent each line of the diff for nice output
		diff, err := o.Diff.DiffOutput()
		if err != nil {
			common.LogWarn("failed to generate diff", map[string]interface{}{"error": err})
		}
		for _, line := range strings.Split(diff, "\n") {
			sb.WriteString(fmt.Sprintf("    %s\n", line))
		}
	}

	sb.WriteString(fmt.Sprintf("  new content: %q\n", o.Diff.After))
	return sb.String()
}

func (lm LineinfileModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	input, ok := params.(LineinfileInput)
	if !ok {
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected LineinfileInput, got %T", params)
	}
	if err := input.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	state := input.State
	if state == "" {
		state = "present" // Default state
	}

	originalContent, err := closure.HostContext.ReadFile(input.Path, runAs)
	originalFileExists := err == nil
	originalMode := "" // Placeholder, implement mode fetching if needed for revert

	if !originalFileExists && !input.Create {
		return nil, fmt.Errorf("file %s does not exist and create=false", input.Path)
	}

	// Handle file creation if requested and file doesn't exist
	if !originalFileExists && input.Create {
		if state == "present" {
			checkMode := closure.IsCheckMode()
			if checkMode {
				common.LogDebug("Would create file %s with line", map[string]interface{}{
					"host": closure.HostContext.Host.Name,
					"path": input.Path,
				})
				return LineinfileOutput{
					Msg:                fmt.Sprintf("(check mode) would create %s with line", input.Path),
					Diff:               pkg.RevertableChange[string]{Before: "", After: input.Line + "\n"},
					OriginalFileExists: false,
					ShouldShowDiff:     pkg.ShouldShowDiff(closure),
				}, nil
			}
			if err := closure.HostContext.WriteFile(input.Path, input.Line+"\n", runAs); err != nil {
				return nil, fmt.Errorf("failed to create and write to file %s: %w", input.Path, err)
			}
			if input.Mode != "" {
				if err := closure.HostContext.SetFileMode(input.Path, input.Mode, runAs); err != nil {
					common.LogWarn(fmt.Sprintf("failed to set mode %s on newly created file %s", input.Mode, input.Path), map[string]interface{}{"error": err})
				}
			}
			return LineinfileOutput{
				Msg:                fmt.Sprintf("file %s created with line", input.Path),
				Diff:               pkg.RevertableChange[string]{Before: "", After: input.Line + "\n"},
				OriginalFileExists: false,
				ShouldShowDiff:     pkg.ShouldShowDiff(closure),
			}, nil
		} else { // state == "absent"
			return LineinfileOutput{
				Msg:                fmt.Sprintf("file %s does not exist, line is already absent", input.Path),
				OriginalFileExists: false,
				ShouldShowDiff:     pkg.ShouldShowDiff(closure),
			}, nil
		}
	} else if !originalFileExists {
		return nil, fmt.Errorf("internal logic error: file %s does not exist but create flag was not handled", input.Path)
	}

	lines := strings.Split(strings.ReplaceAll(originalContent, "\r\n", "\n"), "\n")
	// Remove trailing empty line if present (often an artifact of Split from a final newline)
	if len(lines) > 0 && lines[len(lines)-1] == "" && originalContent != "" {
		lines = lines[:len(lines)-1]
	}

	var compiledRegexp *regexp.Regexp
	if input.Regexp != "" {
		compiledRegexp, err = regexp.Compile(input.Regexp)
		if err != nil {
			return nil, fmt.Errorf("invalid regexp %q: %w", input.Regexp, err)
		}
	}

	modified := false
	found := false
	newLines := make([]string, 0, len(lines))

	// Primary pass: search for regexp or exact line, and apply modifications
	for _, currentLine := range lines {
		match := false
		if compiledRegexp != nil {
			if compiledRegexp.MatchString(currentLine) {
				match = true
			}
		} else if state == "present" && currentLine == input.Line { // Exact match for present state if no regexp
			match = true
		} else if state == "absent" && currentLine == input.Line { // Exact match for absent state if no regexp (and line is specified)
			match = true
		}

		if match {
			found = true
			if state == "present" {
				lineToWrite := input.Line
				if input.Backrefs && compiledRegexp != nil {
					// Convert Ansible-style backreferences (\1) to Go-style ($1)
					backrefPattern := regexp.MustCompile(`\\([0-9]+)`)
					replacementString := backrefPattern.ReplaceAllString(input.Line, "$$$1")
					replacedLine := string(compiledRegexp.ReplaceAllString(currentLine, replacementString))
					if currentLine != replacedLine {
						modified = true
					}
					newLines = append(newLines, replacedLine)
				} else {
					// Replace line or ensure it's the same
					if currentLine != lineToWrite {
						modified = true
					}
					newLines = append(newLines, lineToWrite)
				}
			} else { // state == "absent"
				modified = true // Line removed
				// Do not append currentLine
				continue
			}
		} else {
			newLines = append(newLines, currentLine)
		}
	}

	// Secondary pass: handle insertafter/insertbefore or add if not found (for state=present)
	if state == "present" && !found {
		lineToAdd := input.Line
		if input.InsertAfter != "" {
			idxToInsert := -1
			if strings.ToUpper(input.InsertAfter) == "EOF" {
				idxToInsert = len(newLines)
			} else {
				reAfter, err := regexp.Compile(input.InsertAfter)
				if err != nil {
					return nil, fmt.Errorf("invalid insertafter regexp %q: %w", input.InsertAfter, err)
				}
				for i := len(newLines) - 1; i >= 0; i-- { // Search from bottom for "after"
					if reAfter.MatchString(newLines[i]) {
						idxToInsert = i + 1
						break
					}
				}
				if idxToInsert == -1 { // Not found, append to end as per Ansible behavior
					idxToInsert = len(newLines)
				}
			}
			tempLines := make([]string, 0, len(newLines)+1)
			tempLines = append(tempLines, newLines[:idxToInsert]...)
			tempLines = append(tempLines, lineToAdd)
			tempLines = append(tempLines, newLines[idxToInsert:]...)
			newLines = tempLines
			modified = true
		} else if input.InsertBefore != "" {
			idxToInsert := -1
			if strings.ToUpper(input.InsertBefore) == "BOF" {
				idxToInsert = 0
			} else {
				reBefore, err := regexp.Compile(input.InsertBefore)
				if err != nil {
					return nil, fmt.Errorf("invalid insertbefore regexp %q: %w", input.InsertBefore, err)
				}
				for i := 0; i < len(newLines); i++ { // Search from top for "before"
					if reBefore.MatchString(newLines[i]) {
						idxToInsert = i
						break
					}
				}
				if idxToInsert == -1 { // Not found, prepend as per Ansible behavior (insert at BOF if pattern not found)
					idxToInsert = 0
				}
			}
			tempLines := make([]string, 0, len(newLines)+1)
			if idxToInsert == 0 {
				tempLines = append(tempLines, lineToAdd)
				tempLines = append(tempLines, newLines...)
			} else {
				tempLines = append(tempLines, newLines[:idxToInsert]...)
				tempLines = append(tempLines, lineToAdd)
				tempLines = append(tempLines, newLines[idxToInsert:]...)
			}
			newLines = tempLines
			modified = true
		} else {
			// No insertafter/insertbefore, and line not found. Add to the end of the file.
			newLines = append(newLines, lineToAdd)
			modified = true
		}
	}

	var finalContent string
	// Ensure a single trailing newline if content exists or was modified to have content.
	// If the file becomes empty, it should have no trailing newline.
	if len(newLines) > 0 {
		finalContent = strings.Join(newLines, "\n") + "\n"
	} else if modified { // implies newLines is empty, but original file was not, and we deleted content
		finalContent = "" // explicitly empty
	} else { // not modified, and newLines is empty (original file was empty and remains empty)
		finalContent = originalContent // which is empty string
	}

	if modified {
		checkMode := closure.IsCheckMode()
		if checkMode {
			common.LogDebug("Would modify file %s", map[string]interface{}{
				"host": closure.HostContext.Host.Name,
				"path": input.Path,
			})
		} else {
			if err := closure.HostContext.WriteFile(input.Path, finalContent, runAs); err != nil {
				return nil, fmt.Errorf("failed to write updated content to %s: %w", input.Path, err)
			}
		}
		msg := ""
		if state == "present" {
			if found {
				msg = fmt.Sprintf("line replaced in %s", input.Path)
			} else {
				msg = fmt.Sprintf("line added to %s", input.Path)
			}
		} else { // state == "absent"
			msg = fmt.Sprintf("line removed from %s", input.Path)
		}
		output := LineinfileOutput{
			Msg:                msg,
			Diff:               pkg.RevertableChange[string]{Before: originalContent, After: finalContent},
			OriginalFileExists: originalFileExists,
			OriginalMode:       originalMode,
			ShouldShowDiff:     pkg.ShouldShowDiff(closure),
		}

		return output, nil
	}

	// No modifications made
	msg := ""
	if state == "present" {
		// If line is already present, check if it's *exactly* the input.Line.
		// The `found` flag might be true due to regexp match where the line content is different.
		// However, Ansible considers it 'ok' if regexp matches and state is present, even if line differs.
		// For simplicity here, if `found` (by regexp or exact) and `state==present`, it's 'ok'.
		msg = fmt.Sprintf("line already present in %s", input.Path)
		// If an exact line match was required (no regexp) and found, it's definitely ok.
		// If regexp was used, and it matched, Ansible reports 'ok' not 'changed'.
	} else { // state == "absent"
		if found { // This should ideally not be reached if logic is correct because 'absent' + 'found' should set modified=true
			return nil, fmt.Errorf("internal logic error: line found for absent state but not modified")
		}
		msg = fmt.Sprintf("line already absent from %s", input.Path)
	}
	return LineinfileOutput{
		Msg:                msg,
		Diff:               pkg.RevertableChange[string]{Before: originalContent, After: originalContent},
		OriginalFileExists: originalFileExists,
		OriginalMode:       originalMode,
		ShouldShowDiff:     pkg.ShouldShowDiff(closure),
	}, nil
}

func (lm LineinfileModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previousOutput pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	input, ok := params.(LineinfileInput)
	if !ok {
		return nil, fmt.Errorf("Revert: incorrect parameter type: expected LineinfileInput, got %T", params)
	}

	var prevOutput LineinfileOutput
	if previousOutput != nil {
		prevOutput = previousOutput.(LineinfileOutput)
	} else {
		prevOutput = LineinfileOutput{}
	}

	if !prevOutput.Diff.Changed() && prevOutput.OriginalFileExists {
		return LineinfileOutput{
			Msg:            "no changes to revert",
			ShouldShowDiff: pkg.ShouldShowDiff(closure),
		}, nil
	}

	// If the file was created by the Execute step, revert is to delete it.
	// OriginalFileExists in prevOutput tells us if the file was there *before* the Execute ran.
	if !prevOutput.OriginalFileExists {
		common.DebugOutput("Lineinfile Revert: File %s was created by Execute. Reverting by deleting.", input.Path)
		if _, _, _, err := closure.HostContext.RunCommand(fmt.Sprintf("rm -f %s", fmt.Sprintf("%q", input.Path)), runAs); err != nil {
			return nil, fmt.Errorf("revert failed: could not remove created file %s: %w", input.Path, err)
		}
		return LineinfileOutput{
			Msg:            "reverted file creation",
			ShouldShowDiff: pkg.ShouldShowDiff(closure),
		}, nil
	}

	// File existed before, revert its content (and potentially mode if we tracked it)
	common.DebugOutput("Lineinfile Revert: File %s existed. Reverting content to: %q", input.Path, prevOutput.Diff.Before)
	if err := closure.HostContext.WriteFile(input.Path, prevOutput.Diff.Before, runAs); err != nil {
		return nil, fmt.Errorf("failed to revert content of %s: %w", input.Path, err)
	}

	// TODO: Revert mode if it was changed during creation or modification.
	// This would require fetching and storing originalMode in Execute if file existed,
	// or if file was created and a specific mode was applied.
	// if prevOutput.OriginalMode != "" && prevOutput.OriginalMode != currentModeAfterWrite {
	// closure.HostContext.SetFileMode(input.Path, prevOutput.OriginalMode, runAs)
	// }

	return LineinfileOutput{
		Msg:                fmt.Sprintf("content of %s reverted", input.Path),
		Diff:               pkg.RevertableChange[string]{Before: prevOutput.Diff.After, After: prevOutput.Diff.Before},
		OriginalFileExists: true,                    // It existed before Execute and still exists (content reverted).
		OriginalMode:       prevOutput.OriginalMode, // Carry over if we had it
		ShouldShowDiff:     pkg.ShouldShowDiff(closure),
	}, nil
}

// ParameterAliases defines aliases for module parameters.
func (lm LineinfileModule) ParameterAliases() map[string]string {
	return map[string]string{
		"dest":     "path",
		"destfile": "path",
		"name":     "path",
	}
}

func init() {
	pkg.RegisterModule("lineinfile", LineinfileModule{})
	pkg.RegisterModule("ansible.builtin.lineinfile", LineinfileModule{})
}
