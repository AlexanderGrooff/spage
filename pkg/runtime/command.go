package runtime

import (
	"fmt"
	"io"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/config"
)

// CommandResult represents the result of a command execution
type CommandResult struct {
	Command  string
	ExitCode int
	Stdout   string
	Stderr   string
	Error    error
}

func NewCommandResult(command string, exitCode int, stdout string, stderr string, error error) *CommandResult {
	return &CommandResult{
		Command:  command,
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
		Error:    error,
	}
}

// CommandOptions holds configuration for command execution
type CommandOptions struct {
	Username        string
	UseShell        bool
	Interactive     bool
	UseSudo         bool
	SudoUser        string
	InteractiveSudo bool
	BecomeFlags     string
}

// NewCommandOptions creates a new CommandOptions with sensible defaults
func NewCommandOptions(cfg *config.Config) *CommandOptions {
	return &CommandOptions{
		UseShell:        false,
		Interactive:     false,
		UseSudo:         false,
		InteractiveSudo: false,
		BecomeFlags:     cfg.PrivilegeEscalation.BecomeFlags,
	}
}

// WithUsername sets the username for the command
func (co *CommandOptions) WithUsername(username string) *CommandOptions {
	co.Username = username
	co.UseSudo = username != ""
	return co
}

// WithShell enables shell execution
func (co *CommandOptions) WithShell() *CommandOptions {
	co.UseShell = true
	return co
}

// WithInteractive enables interactive execution
func (co *CommandOptions) WithInteractive() *CommandOptions {
	co.Interactive = true
	return co
}

// WithInteractiveSudo enables interactive sudo
func (co *CommandOptions) WithInteractiveSudo() *CommandOptions {
	co.InteractiveSudo = true
	return co
}

// escapeShellCommand properly escapes a command for use within bash -c '...'
func escapeShellCommand(command string) string {
	// Normalize line endings to Unix format
	escapedCmd := strings.ReplaceAll(command, "\r\n", "\n")

	// Replace backticks with $(...) syntax for better compatibility
	// This handles the case where backticks are used for command substitution
	var result strings.Builder
	var inBackticks bool
	var backtickContent strings.Builder

	for i := 0; i < len(escapedCmd); i++ {
		char := escapedCmd[i]
		if char == '`' {
			if inBackticks {
				// End of backtick section, convert to $()
				result.WriteString("$(")
				result.WriteString(backtickContent.String())
				result.WriteString(")")
				backtickContent.Reset()
				inBackticks = false
			} else {
				// Start of backtick section
				inBackticks = true
				backtickContent.Reset()
			}
		} else if inBackticks {
			backtickContent.WriteByte(char)
		} else {
			result.WriteByte(char)
		}
	}

	// If we have unclosed backticks, just append the content as-is
	if inBackticks {
		result.WriteString("`")
		result.WriteString(backtickContent.String())
	}

	escapedCmd = result.String()

	// Then escape single quotes by replacing ' with '\''
	escapedCmd = strings.ReplaceAll(escapedCmd, "'", "'\\''")

	return escapedCmd
}

// buildCommand constructs the final command string based on options
func buildCommand(command string, opts *CommandOptions) string {
	if command == "" {
		return ""
	}

	// Apply shell escaping if needed
	if opts.UseShell {
		command = escapeShellCommand(command)
	}

	// Build the command based on options
	if !opts.UseSudo {
		if opts.UseShell {
			return fmt.Sprintf("/bin/bash -c '%s'", command)
		}
		return command
	}

	// Handle sudo commands
	if opts.InteractiveSudo {
		if opts.UseShell {
			return fmt.Sprintf("sudo -Su %s %s /bin/bash -c '%s'", opts.Username, opts.BecomeFlags, command)
		}
		return fmt.Sprintf("sudo -Su %s %s", opts.Username, opts.BecomeFlags)
	}

	if opts.UseShell {
		return fmt.Sprintf("sudo -u %s %s /bin/bash -c '%s'", opts.Username, opts.BecomeFlags, command)
	}
	return fmt.Sprintf("sudo -u %s %s", opts.Username, opts.BecomeFlags)
}

// hostWriter adds a host prefix to output for interactive commands
type hostWriter struct {
	writer     io.Writer
	hostPrefix string
}

func (hw *hostWriter) Write(p []byte) (n int, err error) {
	// Write the host prefix first
	if _, err := hw.writer.Write([]byte(hw.hostPrefix)); err != nil {
		return 0, err
	}
	// Then write the actual data
	return hw.writer.Write(p)
}

// cleanSudoPrompts removes sudo password prompts from the output
func cleanSudoPrompts(output string) string {
	lines := strings.Split(output, "\n")
	var cleanedLines []string

	for _, line := range lines {
		// Skip lines that match sudo password prompt patterns
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "[sudo] password for ") ||
			strings.HasPrefix(trimmedLine, "sudo: no tty present") ||
			strings.HasPrefix(trimmedLine, "sudo: no password was provided") ||
			strings.Contains(trimmedLine, "password:") {
			continue
		}
		cleanedLines = append(cleanedLines, line)
	}

	return strings.Join(cleanedLines, "\n")
}

// checkSudoPasswordError checks if the error is due to sudo asking for password
func checkSudoPasswordError(stderrOutput, host string) error {
	if strings.Contains(stderrOutput, "[sudo] password") ||
		strings.Contains(stderrOutput, "sudo: no tty present") ||
		strings.Contains(stderrOutput, "sudo: no password was provided") {
		return fmt.Errorf("sudo requires password input but SSH session is non-interactive on host %s. Consider: 1) Setting up passwordless sudo, 2) Using SSH key authentication, or 3) Running without username parameter", host)
	}
	return nil
}
