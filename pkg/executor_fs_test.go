package pkg

import (
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestChangeCWDToPlaybookDir(t *testing.T) {
	// Test without source FS (should attempt normal chdir)
	playbookPath := "/tmp/test/playbook.yml"
	_, err := ChangeCWDToPlaybookDir(playbookPath)
	assert.NoError(t, err)
	// This might succeed or fail depending on if /tmp/test exists, but shouldn't panic

	// Test with source FS (should be no-op)
	mockFS := &executorMockFS{files: map[string]string{"test.txt": "content"}}
	SetSourceFSForCLI(mockFS)
	defer ClearSourceFSForCLI()

	// FS mode should return current working directory without changing
	_, err = ChangeCWDToPlaybookDir("playbook.yml")
	assert.NoError(t, err)

	// Test with absolute path in FS mode
	_, err = ChangeCWDToPlaybookDir("/absolute/path/playbook.yml")
	assert.NoError(t, err)
}

// executorMockFS for testing executor functionality
type executorMockFS struct {
	files map[string]string
}

func (m *executorMockFS) Open(name string) (fs.File, error) {
	if content, ok := m.files[name]; ok {
		return &executorMockFile{name: name, content: content}, nil
	}
	return nil, fs.ErrNotExist
}

type executorMockFile struct {
	name    string
	content string
	offset  int64
}

func (f *executorMockFile) Stat() (fs.FileInfo, error) {
	return &executorMockFileInfo{name: f.name, size: int64(len(f.content)), isDir: false}, nil
}

func (f *executorMockFile) Read(p []byte) (int, error) {
	if f.offset >= int64(len(f.content)) {
		return 0, fs.ErrClosed
	}
	n := copy(p, f.content[f.offset:])
	f.offset += int64(n)
	return n, nil
}

func (f *executorMockFile) Close() error { return nil }

type executorMockFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (fi *executorMockFileInfo) Name() string       { return fi.name }
func (fi *executorMockFileInfo) Size() int64        { return fi.size }
func (fi *executorMockFileInfo) Mode() fs.FileMode  { return 0644 }
func (fi *executorMockFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *executorMockFileInfo) IsDir() bool        { return fi.isDir }
func (fi *executorMockFileInfo) Sys() interface{}   { return nil }
