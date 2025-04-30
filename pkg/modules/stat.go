package modules

import (
	"fmt"
	"os"
	"reflect"
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

func (i StatInput) HasRevert() bool {
	return false
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

// Function to convert syscall.Timespec to float64 seconds since epoch
func timespecToFloat(ts syscall.Timespec) float64 {
	return float64(ts.Sec) + float64(ts.Nsec)/1e9
}

func (m StatModule) Execute(params pkg.ModuleInput, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	p := params.(StatInput)
	out := StatOutput{}
	out.Stat.Path = p.Path // Set path initially

	// Set default checksum algorithm if it's empty
	checksumAlgo := p.ChecksumAlgorithm
	if checksumAlgo == "" {
		checksumAlgo = "sha1"
	}

	// Pass the follow parameter from the input to c.Stat
	// Note: runAs is currently ignored by c.Stat
	fileInfo, err := closure.HostContext.Stat(p.Path, p.Follow)

	// Handle errors from c.Stat
	if err != nil {
		if os.IsNotExist(err) {
			common.DebugOutput("Stat: Path %s not found: %v", p.Path, err)
			out.Stat.Exists = false
			return out, nil // File not existing is not a module execution error
		} else {
			// Other error during stat (permissions, etc.)
			return nil, fmt.Errorf("failed to stat path %s: %w", p.Path, err)
		}
	}

	// --- File exists, populate StatOutput from os.FileInfo ---
	out.Stat.Exists = true
	out.Stat.Size = fileInfo.Size()
	out.Stat.Mode = fmt.Sprintf("0%o", fileInfo.Mode().Perm()) // Octal permissions

	// File type flags
	mode := fileInfo.Mode()
	out.Stat.IsReg = mode.IsRegular()
	out.Stat.IsDir = mode.IsDir()
	out.Stat.IsLnk = mode&os.ModeSymlink != 0
	out.Stat.IsFifo = mode&os.ModeNamedPipe != 0
	out.Stat.IsSock = mode&os.ModeSocket != 0
	out.Stat.IsChr = mode&os.ModeCharDevice != 0
	out.Stat.IsBlk = mode&os.ModeDevice != 0 && mode&os.ModeCharDevice == 0 // Is device but not char

	common.DebugOutput("Stat: File type flags: %v", out.Stat)
	common.DebugOutput("Stat: File info: %v", fileInfo)
	common.DebugOutput("Stat: File mode: %v", mode)

	// Access OS-specific stat data (syscall.Stat_t)
	sysStat, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok {
		// Could log a warning that detailed info isn't available
		common.LogWarn("Could not get detailed syscall.Stat_t for path", map[string]interface{}{"path": p.Path})
	} else {
		out.Stat.UID = int(sysStat.Uid)
		out.Stat.GID = int(sysStat.Gid)
		out.Stat.Device = uint64(sysStat.Dev)
		out.Stat.Inode = uint64(sysStat.Ino)
		out.Stat.NLink = uint64(sysStat.Nlink)
		out.Stat.Rdev = uint64(sysStat.Rdev)
		out.Stat.Blocks = sysStat.Blocks
		out.Stat.BlockSize = sysStat.Blksize

		// Timestamps
		out.Stat.Atime = timespecToFloat(sysStat.Atim)
		out.Stat.Mtime = timespecToFloat(sysStat.Mtim)
		out.Stat.Ctime = timespecToFloat(sysStat.Ctim)
		// Note: Go's os.FileInfo ModTime() is generally preferred over accessing syscall directly for Mtime
		// out.Stat.Mtime = float64(fileInfo.ModTime().UnixNano()) / 1e9

		// TODO: Lookup Owner and Group names from UID/GID if needed
		// This would likely require c.RunCommand("id -un <uid>") etc. or OS-specific libraries
		// out.Stat.Owner = ...
		// out.Stat.Group = ...
	}

	// --- Optional Getters (Checksum, Mime, Attributes) ---
	// These still require running commands as os.FileInfo doesn't provide them.

	// Get Checksum
	if p.GetChecksum && out.Stat.IsReg { // Only checksum regular files
		checksumCmd := ""
		switch checksumAlgo {
		case "sha1", "md5", "sha224", "sha256", "sha384", "sha512":
			// Quote path using Sprintf %q for basic shell safety
			checksumCmd = fmt.Sprintf("%ssum %s", checksumAlgo, fmt.Sprintf("%q", p.Path))
		default:
			return nil, fmt.Errorf("internal error: unsupported checksum algorithm: %s", checksumAlgo)
		}

		common.DebugOutput("Running checksum: %s", checksumCmd)
		chkStdout, chkStderr, chkErr := closure.HostContext.RunCommand(checksumCmd, runAs)
		if chkErr != nil {
			common.DebugOutput("WARNING: Failed to calculate checksum for %s: %v, stderr: %s", p.Path, chkErr, chkStderr)
		} else {
			parts := strings.Fields(chkStdout)
			if len(parts) > 0 {
				out.Stat.Checksum = parts[0]
			} else {
				common.DebugOutput("WARNING: Could not parse checksum output for %s: %q", p.Path, chkStdout)
			}
		}
	} else if p.GetChecksum {
		common.DebugOutput("Skipping checksum calculation for non-regular file: %s", p.Path)
	}

	// Get Mime Type
	if p.GetMime {
		mimeCmd := "file --brief"
		if p.Follow {
			mimeCmd += " -L" // Follow symlinks
		}
		// Quote path using Sprintf %q
		mimeCmd += fmt.Sprintf(" --mime-type %s", fmt.Sprintf("%q", p.Path))

		common.DebugOutput("Running mime-type: %s", mimeCmd)
		mimeStdout, mimeStderr, mimeErr := closure.HostContext.RunCommand(mimeCmd, runAs)
		if mimeErr != nil {
			common.DebugOutput("WARNING: Failed to get mime type for %s: %v, stderr: %s", p.Path, mimeErr, mimeStderr)
		} else {
			out.Stat.Mime = strings.TrimSpace(mimeStdout)
		}
	}

	// Get Attributes (Linux specific using lsattr)
	if p.GetAttributes {
		// Quote path using Sprintf %q
		attrCmd := fmt.Sprintf("lsattr -d %s", fmt.Sprintf("%q", p.Path))
		common.DebugOutput("Running lsattr: %s", attrCmd)
		attrStdout, attrStderr, attrErr := closure.HostContext.RunCommand(attrCmd, runAs)
		if attrErr != nil {
			if strings.Contains(attrStderr, "command not found") || strings.Contains(attrStderr, "not found") {
				common.DebugOutput("lsattr command not found on host, cannot get attributes for %s", p.Path)
			} else {
				common.DebugOutput("WARNING: Failed to get attributes for %s: %v, stderr: %s", p.Path, attrErr, attrStderr)
			}
		} else {
			parts := strings.Fields(attrStdout)
			if len(parts) > 0 {
				out.Stat.Attributes = parts[0]
			} else {
				common.DebugOutput("WARNING: Could not parse lsattr output for %s: %q", p.Path, attrStdout)
			}
		}
	}

	return out, nil
}

// Stat module is read-only, so Revert does nothing.
func (m StatModule) Revert(params pkg.ModuleInput, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
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
