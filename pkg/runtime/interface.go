package runtime

import "os"

type Connection interface {
	ExecuteCommand(command string, opts *CommandOptions) (*CommandResult, error)
	Stat(path string, follow bool) (os.FileInfo, error)
	WriteFile(filename string, data string) error
	CopyFile(src, dst string) error
	ReadFile(filename string) ([]byte, error)
	SetFileMode(path, modeStr string) error
	Close() error
}
