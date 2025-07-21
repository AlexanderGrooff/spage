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

// StatDetails holds the detailed stat information for a file.
// This struct is used within StatOutput.
type StatDetails struct {
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
}

type StatOutput struct {
	Stat StatDetails `json:"stat"`
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

func (i StatInput) ProvidesVariables() []string {
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

// Function to convert syscall.Timespec to float64 seconds since epoch
func timespecToFloat(ts syscall.Timespec) float64 {
	return float64(ts.Sec) + float64(ts.Nsec)/1e9
}

func (m StatModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	statParams, ok := params.(StatInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected StatInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected StatInput, got %T", params)
	}

	if err := statParams.Validate(); err != nil {
		return nil, err
	}

	out := StatOutput{}
	out.Stat.Path = statParams.Path // Set path initially

	// Set default checksum algorithm if it's empty
	checksumAlgo := statParams.ChecksumAlgorithm
	if checksumAlgo == "" {
		checksumAlgo = "sha1"
	}

	// Pass the follow parameter from the input to c.Stat
	// Note: runAs is currently ignored by c.Stat
	fileInfo, err := closure.HostContext.Stat(statParams.Path, statParams.Follow)

	// Handle errors from c.Stat
	if err != nil {
		if os.IsNotExist(err) {
			common.DebugOutput("Stat: Path %s not found: %v", statParams.Path, err)
			out.Stat.Exists = false
			return out, nil // File not existing is not a module execution error
		} else {
			// Other error during stat (permissions, etc.)
			return nil, fmt.Errorf("failed to stat path %s: %w", statParams.Path, err)
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

	// Access OS-specific stat data (syscall.Stat_t)
	sysStat, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok {
		// Could log a warning that detailed info isn't available
		common.LogWarn("Could not get detailed syscall.Stat_t for path", map[string]interface{}{"path": statParams.Path})
	} else {
		out.Stat.UID = int(sysStat.Uid)
		out.Stat.GID = int(sysStat.Gid)
		out.Stat.Device = uint64(sysStat.Dev)
		out.Stat.Inode = uint64(sysStat.Ino)
		out.Stat.NLink = uint64(sysStat.Nlink)
		out.Stat.Rdev = uint64(sysStat.Rdev)
		out.Stat.Blocks = sysStat.Blocks
		out.Stat.BlockSize = int64(sysStat.Blksize)

		// Assign timestamps using OS-specific implementation
		assignTimestampsOSSpecific(&out.Stat, sysStat)

		// TODO: Lookup Owner and Group names from UID/GID if needed
		// This would likely require c.RunCommand("id -un <uid>") etc. or OS-specific libraries
		// out.Stat.Owner = ...
		// out.Stat.Group = ...
	}

	// --- Optional Getters (Checksum, Mime, Attributes) ---
	// These still require running commands as os.FileInfo doesn't provide them.

	// Get Checksum
	if statParams.GetChecksum && out.Stat.IsReg { // Only checksum regular files
		checksumCmd := ""
		switch checksumAlgo {
		case "sha1", "md5", "sha224", "sha256", "sha384", "sha512":
			// Quote path using Sprintf %q for basic shell safety
			checksumCmd = fmt.Sprintf("%ssum %s", checksumAlgo, fmt.Sprintf("%q", statParams.Path))
		default:
			return nil, fmt.Errorf("internal error: unsupported checksum algorithm: %s", checksumAlgo)
		}

		common.DebugOutput("Running checksum: %s", checksumCmd)
		_, chkStdout, chkStderr, chkErr := closure.HostContext.RunCommand(checksumCmd, runAs)
		if chkErr != nil {
			common.DebugOutput("WARNING: Failed to calculate checksum for %s: %v, stderr: %s", statParams.Path, chkErr, chkStderr)
		} else {
			parts := strings.Fields(chkStdout)
			if len(parts) > 0 {
				out.Stat.Checksum = parts[0]
			} else {
				common.DebugOutput("WARNING: Could not parse checksum output for %s: %q", statParams.Path, chkStdout)
			}
		}
	} else if statParams.GetChecksum {
		common.DebugOutput("Skipping checksum calculation for non-regular file: %s", statParams.Path)
	}

	// Get Mime Type
	if statParams.GetMime {
		mimeCmd := fmt.Sprintf("file --brief --mime-type %s", fmt.Sprintf("%q", statParams.Path))
		common.DebugOutput("Running mime type check: %s", mimeCmd)
		_, mimeStdout, mimeStderr, mimeErr := closure.HostContext.RunCommand(mimeCmd, runAs)
		if mimeErr != nil {
			common.DebugOutput("WARNING: Failed to get mime type for %s: %v, stderr: %s", statParams.Path, mimeErr, mimeStderr)
		} else {
			out.Stat.Mime = strings.TrimSpace(mimeStdout)
		}
	}

	// Get File Attributes (Linux specific example with 'lsattr')
	if statParams.GetAttributes {
		// This is highly OS-specific. Example for Linux:
		attrCmd := fmt.Sprintf("lsattr -d %s", fmt.Sprintf("%q", statParams.Path))
		common.DebugOutput("Running attribute check: %s", attrCmd)
		_, attrStdout, attrStderr, attrErr := closure.HostContext.RunCommand(attrCmd, runAs)
		if attrErr != nil {
			common.DebugOutput("WARNING: Failed to get attributes for %s: %v, stderr: %s", statParams.Path, attrErr, attrStderr)
		} else {
			// lsattr output is like "----i---------e---- /path/to/file"
			parts := strings.Fields(attrStdout)
			if len(parts) > 0 {
				out.Stat.Attributes = parts[0]
			} else {
				common.DebugOutput("WARNING: Could not parse lsattr output for %s: %q", statParams.Path, attrStdout)
			}
		}
	}

	return out, nil
}

// Stat module is read-only, so Revert does nothing.
func (m StatModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	// Stat module is read-only, so revert is a no-op.
	common.LogDebug("Revert called for stat module (no-op)", map[string]interface{}{})
	// Return the previous output if available, otherwise a new non-changed output.
	if previous != nil {
		return previous, nil
	}
	// Construct a default StatOutput. Its Changed() method will return false.
	return StatOutput{Stat: StatDetails{}}, nil
}

// assignTimestampsOSSpecific assigns Atime, Mtime, and Ctime to the StatDetails
// based on the OS-specific fields in syscall.Stat_t.
// Implementations are provided in stat_linux.go and stat_darwin.go.

func init() {
	// Ensure syscall package usage is minimal or abstract if targeting non-linux compiles for spage itself.
	// Currently only used for exit code check potentially. Consider removing if HostContext provides codes directly.
	_ = syscall.Exit // Example usage to avoid unused import error if checks change

	pkg.RegisterModule("stat", StatModule{})
	pkg.RegisterModule("ansible.builtin.stat", StatModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m StatModule) ParameterAliases() map[string]string {
	return nil // No aliases defined for this module
}
