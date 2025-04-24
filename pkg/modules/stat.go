package modules

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"syscall"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
)

type StatModule struct{}

func (sm StatModule) InputType() reflect.Type {
	return reflect.TypeOf(StatInput{})
}

func (sm StatModule) OutputType() reflect.Type {
	return reflect.TypeOf(StatOutput{})
}

type StatInput struct {
	Path              string `yaml:"path"`
	ChecksumAlgorithm string `yaml:"checksum_algorithm,omitempty"`
	Follow            bool   `yaml:"follow,omitempty"`
	GetAttributes     bool   `yaml:"get_attributes,omitempty"`
	GetChecksum       bool   `yaml:"get_checksum,omitempty"`
	GetMime           bool   `yaml:"get_mime,omitempty"`
	pkg.ModuleInput
}

type StatOutput struct {
	Stat struct {
		Exists     bool    `json:"exists"`
		Path       string  `json:"path"`
		Mode       string  `json:"mode,omitempty"`
		UID        int     `json:"uid,omitempty"`
		GID        int     `json:"gid,omitempty"`
		Size       int64   `json:"size,omitempty"`
		IsLnk      bool    `json:"islnk,omitempty"`
		IsReg      bool    `json:"isreg,omitempty"`
		IsDir      bool    `json:"isdir,omitempty"`
		IsFifo     bool    `json:"isfifo,omitempty"`
		IsBlk      bool    `json:"isblk,omitempty"`
		IsChr      bool    `json:"ischr,omitempty"`
		IsSock     bool    `json:"issock,omitempty"`
		Atime      float64 `json:"atime,omitempty"`
		Mtime      float64 `json:"mtime,omitempty"`
		Ctime      float64 `json:"ctime,omitempty"`
		Checksum   string  `json:"checksum,omitempty"`
		Mime       string  `json:"mime,omitempty"`
		Attributes string  `json:"attributes,omitempty"`
		Device     uint64  `json:"dev,omitempty"`        // Device number
		Inode      uint64  `json:"inode,omitempty"`      // Inode number
		NLink      uint64  `json:"nlink,omitempty"`      // Number of hard links
		Rdev       uint64  `json:"rdev,omitempty"`       // Device number (if special file)
		Blocks     int64   `json:"blocks,omitempty"`     // Number of blocks allocated
		BlockSize  int64   `json:"block_size,omitempty"` // Block size
		Owner      string  `json:"owner,omitempty"`
		Group      string  `json:"group,omitempty"`
		// TODO: selinux context fields if possible/needed
	} `json:"stat"`
	pkg.ModuleOutput
}

func (i StatInput) ToCode() string {
	// Use %q for strings, %t for bools
	return fmt.Sprintf("modules.StatInput{Path: %q, ChecksumAlgorithm: %q, Follow: %t, GetAttributes: %t, GetChecksum: %t, GetMime: %t}",
		i.Path,
		i.ChecksumAlgorithm,
		i.Follow,
		i.GetAttributes,
		i.GetChecksum,
		i.GetMime,
	)
}

func (i StatInput) GetVariableUsage() []string {
	vars := pkg.GetVariableUsageFromTemplate(i.Path)
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.ChecksumAlgorithm)...)
	// Follow, GetAttributes, GetChecksum, GetMime are booleans, typically not templated in Ansible
	return vars
}

func (i StatInput) Validate() error {
	if i.Path == "" {
		return fmt.Errorf("missing Path input")
	}
	if i.ChecksumAlgorithm == "" {
		i.ChecksumAlgorithm = "sha1" // Default checksum algorithm
	}
	validAlgos := map[string]bool{"sha1": true, "md5": true, "sha224": true, "sha256": true, "sha384": true, "sha512": true}
	if i.GetChecksum && !validAlgos[i.ChecksumAlgorithm] {
		return fmt.Errorf("invalid ChecksumAlgorithm: %s", i.ChecksumAlgorithm)
	}
	// Default get_* values are typically true in Ansible, but let's stick to Go defaults (false) unless specified.
	// Ansible defaults: get_attributes=yes, get_checksum=yes, get_mime=yes, follow=no
	// For simplicity here, we'll require them to be set explicitly if needed, matching Go's zero values.
	// If we want true Ansible parity, we'd need to handle yaml parsing defaults or set them here. Let's assume explicit for now.
	return nil
}

func (o StatOutput) String() string {
	if !o.Stat.Exists {
		return fmt.Sprintf("path %s does not exist", o.Stat.Path)
	}
	parts := []string{fmt.Sprintf("path %s exists", o.Stat.Path)}
	if o.Stat.IsReg {
		parts = append(parts, "type: file")
	} else if o.Stat.IsDir {
		parts = append(parts, "type: directory")
	} else if o.Stat.IsLnk {
		parts = append(parts, "type: link")
	} // Add other types if needed
	parts = append(parts, fmt.Sprintf("mode: %s", o.Stat.Mode))
	parts = append(parts, fmt.Sprintf("size: %d", o.Stat.Size))
	if o.Stat.Checksum != "" {
		parts = append(parts, fmt.Sprintf("checksum: %s", o.Stat.Checksum))
	}
	if o.Stat.Mime != "" {
		parts = append(parts, fmt.Sprintf("mime: %s", o.Stat.Mime))
	}
	// Add more fields as needed for a concise summary
	return strings.Join(parts, ", ")
}

// Stat module never changes state
func (o StatOutput) Changed() bool {
	return false
}

func parseStatOutput(output string) (*StatOutput, error) {
	// Expected format: %a %u %g %s %W %X %Y %Z %F %N %d %i %h %r %b %B %U %G
	// OctalPerms UID GID Size CreationTime AccessTime ModifyTime ChangeTime FileType FileName DeviceNumber InodeNum HardLinks RawDeviceNum AllocBlocks BlockSize UserName GroupName
	fields := strings.Split(strings.TrimSpace(output), "\n")
	if len(fields) < 16 { // Check minimum expected fields based on format string below
		return nil, fmt.Errorf("unexpected stat output format: received %d fields, expected at least 16. Output: %q", len(fields), output)
	}

	out := &StatOutput{}
	out.Stat.Exists = true // If we got output, it exists

	var err error
	out.Stat.Mode = fields[0]                                    // %a Octal permissions
	if out.Stat.UID, err = strconv.Atoi(fields[1]); err != nil { // %u UID
		return nil, fmt.Errorf("failed to parse UID: %v", err)
	}
	if out.Stat.GID, err = strconv.Atoi(fields[2]); err != nil { // %g GID
		return nil, fmt.Errorf("failed to parse GID: %v", err)
	}
	if out.Stat.Size, err = strconv.ParseInt(fields[3], 10, 64); err != nil { // %s Size
		return nil, fmt.Errorf("failed to parse Size: %v", err)
	}
	// %W Creation time (birth) - skip for now, often 0
	if out.Stat.Atime, err = strconv.ParseFloat(fields[5], 64); err != nil { // %X Access time epoch
		return nil, fmt.Errorf("failed to parse Atime: %v", err)
	}
	if out.Stat.Mtime, err = strconv.ParseFloat(fields[6], 64); err != nil { // %Y Modify time epoch
		return nil, fmt.Errorf("failed to parse Mtime: %v", err)
	}
	if out.Stat.Ctime, err = strconv.ParseFloat(fields[7], 64); err != nil { // %Z Change time epoch
		return nil, fmt.Errorf("failed to parse Ctime: %v", err)
	}

	// %F File type
	fileType := fields[8]
	out.Stat.IsReg = fileType == "regular file" || fileType == "regular empty file"
	out.Stat.IsDir = fileType == "directory"
	out.Stat.IsLnk = fileType == "symbolic link"
	out.Stat.IsFifo = fileType == "fifo"
	out.Stat.IsSock = fileType == "socket"
	out.Stat.IsChr = fileType == "character special file"
	out.Stat.IsBlk = fileType == "block special file"

	// %N File name (includes link target if symlink) - Use input path for consistency
	out.Stat.Path = fields[9] // Actually the path resolved by stat, might differ slightly from input

	// %d Device number (decimal)
	if out.Stat.Device, err = strconv.ParseUint(fields[10], 10, 64); err != nil {
		return nil, fmt.Errorf("failed to parse Device: %v", err)
	}
	// %i Inode number (decimal)
	if out.Stat.Inode, err = strconv.ParseUint(fields[11], 10, 64); err != nil {
		return nil, fmt.Errorf("failed to parse Inode: %v", err)
	}
	// %h Number of hard links
	if out.Stat.NLink, err = strconv.ParseUint(fields[12], 10, 64); err != nil {
		return nil, fmt.Errorf("failed to parse NLink: %v", err)
	}
	// %r Raw device number (hexadecimal) - convert from hex
	if fields[13] != "?" { // stat outputs '?' if not applicable
		if out.Stat.Rdev, err = strconv.ParseUint(fields[13], 16, 64); err != nil {
			return nil, fmt.Errorf("failed to parse Rdev (hex): %v", err)
		}
	}
	// %b Number of blocks allocated (like du)
	if out.Stat.Blocks, err = strconv.ParseInt(fields[14], 10, 64); err != nil {
		return nil, fmt.Errorf("failed to parse Blocks: %v", err)
	}
	// %B The size in bytes of each block reported by %b
	if out.Stat.BlockSize, err = strconv.ParseInt(fields[15], 10, 64); err != nil {
		return nil, fmt.Errorf("failed to parse BlockSize: %v", err)
	}
	// %U User name of owner
	out.Stat.Owner = fields[16]
	// %G Group name of owner
	out.Stat.Group = fields[17]

	return out, nil
}

func (m StatModule) Execute(params pkg.ModuleInput, c *pkg.HostContext, runAs string) (pkg.ModuleOutput, error) {
	p := params.(StatInput)

	// TODO: pass p.Follow
	stdout, stderr, err := c.Stat(p.Path, runAs)

	// Handle "file not found" or other errors during stat
	if err != nil {
		// Check if the error is specifically "No such file or directory"
		// This check might need refinement based on actual stderr content across systems
		if strings.Contains(stderr, "No such file or directory") || strings.Contains(stderr, "cannot stat") {
			common.DebugOutput("Stat: Path %s not found (stderr: %s)", p.Path, stderr)
			// Return Exists: false
			out := &StatOutput{}
			out.Stat.Path = p.Path // Keep the original path
			out.Stat.Exists = false
			return *out, nil // Not an error from the module's perspective, just file not found
		}
		// Otherwise, it's a different error
		return nil, fmt.Errorf("failed to run stat command on %s: %v, stderr: %s", p.Path, err, stderr)
	}

	// Parse the main stat output
	out, err := parseStatOutput(stdout)
	if err != nil {
		return nil, fmt.Errorf("failed to parse stat output for %s: %v", p.Path, err)
	}
	// Ensure the path in the output matches the input path for clarity, as %N might resolve links etc.
	out.Stat.Path = p.Path

	// --- Optional Getters ---

	// Get Checksum
	if p.GetChecksum {
		checksumCmd := ""
		switch p.ChecksumAlgorithm {
		case "sha1", "md5", "sha224", "sha256", "sha384", "sha512":
			// Note: No shell escaping applied here for simplicity. Assumes path is safe.
			checksumCmd = fmt.Sprintf("%ssum %s", p.ChecksumAlgorithm, p.Path)
		default:
			return nil, fmt.Errorf("unsupported checksum algorithm: %s", p.ChecksumAlgorithm) // Should be caught by Validate, but double-check
		}

		// If the file is a directory, checksum command will likely fail or checksum the contents recursively.
		// Ansible's stat returns checksum only for regular files.
		if out.Stat.IsReg {
			common.DebugOutput("Running checksum: %s", checksumCmd)
			chkStdout, chkStderr, chkErr := c.RunCommand(checksumCmd, runAs)
			if chkErr != nil {
				// Don't fail the whole module, just log or skip checksum
				common.DebugOutput("WARNING: Failed to calculate checksum for %s: %v, stderr: %s", p.Path, chkErr, chkStderr)
			} else {
				// Output format is typically "checksum  filename"
				parts := strings.Fields(chkStdout)
				if len(parts) > 0 {
					out.Stat.Checksum = parts[0]
				} else {
					common.DebugOutput("WARNING: Could not parse checksum output for %s: %q", p.Path, chkStdout)
				}
			}
		} else {
			common.DebugOutput("Skipping checksum calculation for non-regular file: %s", p.Path)
		}
	}

	// Get Mime Type
	if p.GetMime {
		mimeCmd := "file --brief"
		if p.Follow {
			mimeCmd += " -L" // Follow symlinks
		}
		// Note: No shell escaping applied here for simplicity. Assumes path is safe.
		mimeCmd += " --mime-type " + p.Path

		common.DebugOutput("Running mime-type: %s", mimeCmd)
		mimeStdout, mimeStderr, mimeErr := c.RunCommand(mimeCmd, runAs)
		if mimeErr != nil {
			common.DebugOutput("WARNING: Failed to get mime type for %s: %v, stderr: %s", p.Path, mimeErr, mimeStderr)
		} else {
			out.Stat.Mime = strings.TrimSpace(mimeStdout)
		}
	}

	// Get Attributes (Linux specific using lsattr)
	if p.GetAttributes {
		// lsattr might not exist or work on non-Linux systems.
		// Ansible module likely has more robust platform detection.
		// We'll try it and warn if it fails.
		// -d is needed for directories, otherwise it lists contents. Works for files too.
		// Note: No shell escaping applied here for simplicity. Assumes path is safe.
		attrCmd := "lsattr -d " + p.Path
		common.DebugOutput("Running lsattr: %s", attrCmd)
		attrStdout, attrStderr, attrErr := c.RunCommand(attrCmd, runAs)
		if attrErr != nil {
			// Check if lsattr command not found, common case.
			if strings.Contains(attrStderr, "command not found") || strings.Contains(attrStderr, "not found") {
				common.DebugOutput("lsattr command not found on host, cannot get attributes for %s", p.Path)
			} else {
				common.DebugOutput("WARNING: Failed to get attributes for %s: %v, stderr: %s", p.Path, attrErr, attrStderr)
			}
		} else {
			// Output is typically "Attributes  Filename"
			parts := strings.Fields(attrStdout)
			if len(parts) > 0 {
				out.Stat.Attributes = parts[0]
			} else {
				common.DebugOutput("WARNING: Could not parse lsattr output for %s: %q", p.Path, attrStdout)
			}
		}
	}

	return *out, nil
}

// Stat module is read-only, so Revert does nothing.
func (m StatModule) Revert(params pkg.ModuleInput, c *pkg.HostContext, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	common.DebugOutput("Stat module does not support revert.")
	// Return an empty output, indicating no change was reverted.
	return StatOutput{
		Stat: struct {
			Exists     bool    `json:"exists"`
			Path       string  `json:"path"`
			Mode       string  `json:"mode,omitempty"`
			UID        int     `json:"uid,omitempty"`
			GID        int     `json:"gid,omitempty"`
			Size       int64   `json:"size,omitempty"`
			IsLnk      bool    `json:"islnk,omitempty"`
			IsReg      bool    `json:"isreg,omitempty"`
			IsDir      bool    `json:"isdir,omitempty"`
			IsFifo     bool    `json:"isfifo,omitempty"`
			IsBlk      bool    `json:"isblk,omitempty"`
			IsChr      bool    `json:"ischr,omitempty"`
			IsSock     bool    `json:"issock,omitempty"`
			Atime      float64 `json:"atime,omitempty"`
			Mtime      float64 `json:"mtime,omitempty"`
			Ctime      float64 `json:"ctime,omitempty"`
			Checksum   string  `json:"checksum,omitempty"`
			Mime       string  `json:"mime,omitempty"`
			Attributes string  `json:"attributes,omitempty"`
			Device     uint64  `json:"dev,omitempty"`
			Inode      uint64  `json:"inode,omitempty"`
			NLink      uint64  `json:"nlink,omitempty"`
			Rdev       uint64  `json:"rdev,omitempty"`
			Blocks     int64   `json:"blocks,omitempty"`
			BlockSize  int64   `json:"block_size,omitempty"`
			Owner      string  `json:"owner,omitempty"`
			Group      string  `json:"group,omitempty"`
		}{
			Path:   params.(StatInput).Path, // Include path for context
			Exists: false,                   // Indicate nothing exists post-revert (conceptually)
		},
	}, nil
}

func init() {
	// Ensure syscall package usage is minimal or abstract if targeting non-linux compiles for spage itself.
	// Currently only used for exit code check potentially. Consider removing if HostContext provides codes directly.
	_ = syscall.Exit // Example usage to avoid unused import error if checks change

	pkg.RegisterModule("stat", StatModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m StatModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
