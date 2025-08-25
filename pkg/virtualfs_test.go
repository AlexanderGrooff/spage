package pkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMemFSFromTarGz(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string // path -> content
		dirs        []string          // directory paths
		expectError bool
	}{
		{
			name: "simple files and dirs",
			files: map[string]string{
				"playbook.yml":                   "---\n- hosts: all\n  tasks:\n    - name: test\n      debug:\n        msg: hello",
				"roles/test_role/tasks/main.yml": "---\n- name: role task\n  debug:\n    msg: from role",
				"templates/test.conf.j2":         "config_value: {{ variable }}",
			},
			dirs: []string{
				"roles",
				"roles/test_role",
				"roles/test_role/tasks",
				"templates",
			},
		},
		{
			name: "nested directory structure",
			files: map[string]string{
				"deep/nested/path/file.txt": "content",
				"another/deep/file.yml":     "yaml content",
			},
			dirs: []string{
				"deep",
				"deep/nested",
				"deep/nested/path",
				"another",
				"another/deep",
			},
		},
		{
			name:  "empty archive",
			files: map[string]string{},
			dirs:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create tar.gz in memory
			var buf bytes.Buffer
			gw := gzip.NewWriter(&buf)
			tw := tar.NewWriter(gw)

			// Add directories first
			for _, dir := range tt.dirs {
				hdr := &tar.Header{
					Name:     dir,
					Typeflag: tar.TypeDir,
					Mode:     0755,
					ModTime:  time.Now(),
				}
				require.NoError(t, tw.WriteHeader(hdr))
			}

			// Add files
			for path, content := range tt.files {
				hdr := &tar.Header{
					Name:     path,
					Typeflag: tar.TypeReg,
					Mode:     0644,
					Size:     int64(len(content)),
					ModTime:  time.Now(),
				}
				require.NoError(t, tw.WriteHeader(hdr))
				_, err := tw.Write([]byte(content))
				require.NoError(t, err)
			}

			require.NoError(t, tw.Close())
			require.NoError(t, gw.Close())

			// Test loading into MemFS
			memfs, err := NewMemFSFromTarGz(&buf)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, memfs)

			// Verify directories exist
			for _, dir := range tt.dirs {
				info, err := fs.Stat(memfs, dir)
				require.NoError(t, err, "dir %s should exist", dir)
				assert.True(t, info.IsDir(), "path %s should be a directory", dir)
			}

			// Verify files exist with correct content
			for path, expectedContent := range tt.files {
				info, err := fs.Stat(memfs, path)
				require.NoError(t, err, "file %s should exist", path)
				assert.False(t, info.IsDir(), "path %s should be a file", path)
				assert.Equal(t, int64(len(expectedContent)), info.Size())

				// Read and verify content
				content, err := fs.ReadFile(memfs, path)
				require.NoError(t, err)
				assert.Equal(t, expectedContent, string(content))
			}

			// Test root directory access
			root, err := memfs.Open(".")
			require.NoError(t, err)
			defer func() { _ = root.Close() }()
			rootInfo, err := root.Stat()
			require.NoError(t, err)
			assert.True(t, rootInfo.IsDir())
		})
	}
}

func TestMemFS_Open(t *testing.T) {
	// Create a simple MemFS with test content
	memfs := &MemFS{
		files: map[string]*memEntry{
			"file.txt": {
				isDir:   false,
				data:    []byte("test content"),
				mode:    0644,
				modTime: time.Now(),
			},
			"dir": {
				isDir:   true,
				mode:    0755,
				modTime: time.Now(),
			},
			"nested/file.yml": {
				isDir:   false,
				data:    []byte("yaml content"),
				mode:    0644,
				modTime: time.Now(),
			},
		},
	}

	// Ensure parent directories exist
	memfs.ensureDir("nested")

	tests := []struct {
		name        string
		path        string
		expectError bool
		expectDir   bool
		expectSize  int64
	}{
		{"root", ".", false, true, 0},
		{"existing file", "file.txt", false, false, 12},
		{"existing dir", "dir", false, true, 0},
		{"nested file", "nested/file.yml", false, false, 12},
		{"nonexistent", "nonexistent.txt", true, false, 0},
		{"clean path", "./file.txt", false, false, 12},
		{"clean nested", "./nested/./file.yml", false, false, 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := memfs.Open(tt.path)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			defer func() { _ = file.Close() }()

			info, err := file.Stat()
			require.NoError(t, err)
			assert.Equal(t, tt.expectDir, info.IsDir())
			assert.Equal(t, tt.expectSize, info.Size())
		})
	}
}

func TestMemFS_ReadFile(t *testing.T) {
	memfs := &MemFS{
		files: map[string]*memEntry{
			"test.txt": {
				isDir:   false,
				data:    []byte("hello world"),
				mode:    0644,
				modTime: time.Now(),
			},
		},
	}

	// Test successful read
	content, err := fs.ReadFile(memfs, "test.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))

	// Test reading nonexistent file
	_, err = fs.ReadFile(memfs, "nonexistent.txt")
	assert.Error(t, err)
}

func TestMemFS_Stat(t *testing.T) {
	now := time.Now()
	memfs := &MemFS{
		files: map[string]*memEntry{
			"file.txt": {
				isDir:   false,
				data:    []byte("content"),
				mode:    0644,
				modTime: now,
			},
			"dir": {
				isDir:   true,
				mode:    0755,
				modTime: now,
			},
		},
	}

	// Test file stat
	fileInfo, err := fs.Stat(memfs, "file.txt")
	require.NoError(t, err)
	assert.False(t, fileInfo.IsDir())
	assert.Equal(t, int64(7), fileInfo.Size())
	assert.Equal(t, fs.FileMode(0644), fileInfo.Mode())
	assert.Equal(t, now, fileInfo.ModTime())

	// Test directory stat
	dirInfo, err := fs.Stat(memfs, "dir")
	require.NoError(t, err)
	assert.True(t, dirInfo.IsDir())
	assert.Equal(t, fs.FileMode(0755), dirInfo.Mode())

	// Test nonexistent path
	_, err = fs.Stat(memfs, "nonexistent")
	assert.Error(t, err)
}

func TestMemFS_ensureDir(t *testing.T) {
	memfs := &MemFS{files: make(map[string]*memEntry)}

	// Test creating nested directory structure
	memfs.ensureDir("a/b/c/d")

	// Verify all parent directories were created
	expectedDirs := []string{"a", "a/b", "a/b/c", "a/b/c/d"}
	for _, dir := range expectedDirs {
		info, err := fs.Stat(memfs, dir)
		require.NoError(t, err, "directory %s should exist", dir)
		assert.True(t, info.IsDir())
	}

	// Test creating directory with relative paths
	memfs.ensureDir("./relative/../path")
	info, err := fs.Stat(memfs, "path")
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestMemFS_OpenFile_Read(t *testing.T) {
	memfs := &MemFS{
		files: map[string]*memEntry{
			"readme.txt": {
				isDir:   false,
				data:    []byte("this is a test file\nwith multiple lines"),
				mode:    0644,
				modTime: time.Now(),
			},
		},
	}

	file, err := memfs.Open("readme.txt")
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	// Test reading in chunks
	buf := make([]byte, 10)
	n, err := file.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 10, n)
	assert.Equal(t, "this is a ", string(buf))

	// Test reading to end
	remaining, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, "test file\nwith multiple lines", string(remaining))

	// Test reading from directory (should return EOF)
	dir, err := memfs.Open(".")
	require.NoError(t, err)
	defer func() { _ = dir.Close() }()
	n, err = dir.Read(buf)
	assert.Equal(t, 0, n)
	assert.Equal(t, io.EOF, err)
}

func TestMemFS_OpenFile_Stat(t *testing.T) {
	now := time.Now()
	memfs := &MemFS{
		files: map[string]*memEntry{
			"config.yml": {
				isDir:   false,
				data:    []byte("key: value"),
				mode:    0644,
				modTime: now,
			},
		},
	}

	file, err := memfs.Open("config.yml")
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	require.NoError(t, err)
	assert.Equal(t, "config.yml", info.Name())
	assert.False(t, info.IsDir())
	assert.Equal(t, int64(10), info.Size())
	assert.Equal(t, fs.FileMode(0644), info.Mode())
	assert.Equal(t, now, info.ModTime())
}

func TestMemFS_InvalidPaths(t *testing.T) {
	memfs := &MemFS{files: make(map[string]*memEntry)}

	// Test path traversal prevention
	_, err := memfs.Open("../etc/passwd")
	assert.Error(t, err)

	_, err = memfs.Open("../../../etc/passwd")
	assert.Error(t, err)

	// Test absolute paths (should be normalized)
	_, err = memfs.Open("/absolute/path")
	assert.Error(t, err)
}

func TestNewMemFSFromTarGz_InvalidArchive(t *testing.T) {
	// Test with invalid gzip data
	invalidData := []byte("not a gzip file")
	_, err := NewMemFSFromTarGz(bytes.NewReader(invalidData))
	assert.Error(t, err)

	// Test with empty reader
	_, err = NewMemFSFromTarGz(bytes.NewReader(nil))
	assert.Error(t, err)
}

func TestNewMemFSFromTarGz_PathTraversal(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Try to create a file with path traversal
	data := []byte("malicious")
	hdr := &tar.Header{
		Name:     "../../../etc/passwd",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(data)),
		ModTime:  time.Now(),
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, err := tw.Write(data)
	require.NoError(t, err)

	// Close writers in correct order
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	// Should reject path traversal
	_, err = NewMemFSFromTarGz(&buf)
	assert.Error(t, err)
}
