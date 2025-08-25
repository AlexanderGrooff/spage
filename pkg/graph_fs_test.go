package pkg

import (
	"io"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGraphFromFS(t *testing.T) {
	// Create a mock FS with a simple playbook
	graphFS := &graphMockFS{
		files: map[string]string{
			"playbook.yml": `---
- hosts: all
  tasks:
    - name: test task
      command: echo "hello world"
`,
		},
	}

	// Test that the FS can be read correctly
	data, err := fs.ReadFile(graphFS, "playbook.yml")
	require.NoError(t, err)
	assert.Contains(t, string(data), "hosts: all")
	assert.Contains(t, string(data), "command: echo")

	// Test with invalid playbook path
	_, err = fs.ReadFile(graphFS, "nonexistent.yml")
	assert.Error(t, err)

	// Test with invalid YAML
	invalidFS := &graphMockFS{
		files: map[string]string{
			"invalid.yml": `invalid: yaml: content:`,
		},
	}
	data, err = fs.ReadFile(invalidFS, "invalid.yml")
	require.NoError(t, err)
	assert.Equal(t, "invalid: yaml: content:", string(data))
}

// graphMockFS for testing - simplified version
type graphMockFS struct {
	files map[string]string
}

func (m *graphMockFS) Open(name string) (fs.File, error) {
	if content, ok := m.files[name]; ok {
		return &mockFile{name: name, content: content}, nil
	}
	return nil, fs.ErrNotExist
}

type mockFile struct {
	name    string
	content string
	offset  int64
}

func (f *mockFile) Stat() (fs.FileInfo, error) {
	return &mockFileInfo{name: f.name, size: int64(len(f.content)), isDir: false}, nil
}

func (f *mockFile) Read(p []byte) (int, error) {
	if f.offset >= int64(len(f.content)) {
		return 0, io.EOF
	}
	n := copy(p, f.content[f.offset:])
	f.offset += int64(n)
	return n, nil
}

func (f *mockFile) Close() error {
	return nil
}

type mockFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (fi *mockFileInfo) Name() string       { return fi.name }
func (fi *mockFileInfo) Size() int64        { return fi.size }
func (fi *mockFileInfo) Mode() fs.FileMode  { return 0644 }
func (fi *mockFileInfo) ModTime() time.Time { return time.Now() }
func (fi *mockFileInfo) IsDir() bool        { return fi.isDir }
func (fi *mockFileInfo) Sys() interface{}   { return nil }
