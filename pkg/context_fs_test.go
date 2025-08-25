package pkg

import (
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceFSFunctions(t *testing.T) {
	// Test initial state
	assert.Nil(t, getSourceFS())

	// Test setting and getting
	mockFS := &contextMockFS{files: map[string]string{"test.txt": "content"}}
	SetSourceFSForCLI(mockFS)
	assert.Equal(t, mockFS, getSourceFS())

	// Test clearing
	ClearSourceFSForCLI()
	assert.Nil(t, getSourceFS())
}

func TestReadTemplateFile(t *testing.T) {
	// Test without source FS (should fall back to local)
	_, err := ReadTemplateFile("nonexistent.txt")
	assert.Error(t, err) // Should fail since file doesn't exist

	// Test with source FS
	mockFS := &contextMockFS{
		files: map[string]string{
			"templates/test.conf.j2": "template content",
		},
	}
	SetSourceFSForCLI(mockFS)
	defer ClearSourceFSForCLI()

	// Test reading from templates directory
	content, err := ReadTemplateFile("test.conf.j2")
	require.NoError(t, err)
	assert.Equal(t, "template content", content)

	// Test reading with absolute path (should still work)
	content, err = ReadTemplateFile("/templates/test.conf.j2")
	require.NoError(t, err)
	assert.Equal(t, "template content", content)
}

func TestReadSourceFile(t *testing.T) {
	// Test without source FS (should fall back to local)
	_, err := ReadSourceFile("nonexistent.txt")
	assert.Error(t, err) // Should fail since file doesn't exist

	// Test with source FS
	mockFS := &contextMockFS{
		files: map[string]string{
			"config.yml": "config content",
		},
	}
	SetSourceFSForCLI(mockFS)
	defer ClearSourceFSForCLI()

	// Test reading from source FS
	content, err := ReadSourceFile("config.yml")
	require.NoError(t, err)
	assert.Equal(t, "config content", content)

	// Test reading with relative path
	content, err = ReadSourceFile("./config.yml")
	require.NoError(t, err)
	assert.Equal(t, "config content", content)
}

// contextMockFS for testing context functionality
type contextMockFS struct {
	files map[string]string
}

func (m *contextMockFS) Open(name string) (fs.File, error) {
	if content, ok := m.files[name]; ok {
		return &contextMockFile{name: name, content: content}, nil
	}
	return nil, fs.ErrNotExist
}

type contextMockFile struct {
	name    string
	content string
	offset  int64
}

func (f *contextMockFile) Stat() (fs.FileInfo, error) {
	return &contextMockFileInfo{name: f.name, size: int64(len(f.content)), isDir: false}, nil
}

func (f *contextMockFile) Read(p []byte) (int, error) {
	if f.offset >= int64(len(f.content)) {
		return 0, fs.ErrClosed
	}
	n := copy(p, f.content[f.offset:])
	f.offset += int64(n)
	return n, nil
}

func (f *contextMockFile) Close() error { return nil }

type contextMockFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (fi *contextMockFileInfo) Name() string       { return fi.name }
func (fi *contextMockFileInfo) Size() int64        { return fi.size }
func (fi *contextMockFileInfo) Mode() fs.FileMode  { return 0644 }
func (fi *contextMockFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *contextMockFileInfo) IsDir() bool        { return fi.isDir }
func (fi *contextMockFileInfo) Sys() interface{}   { return nil }
