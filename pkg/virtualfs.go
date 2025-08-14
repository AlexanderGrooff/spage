package pkg

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// memEntry holds either a file or directory entry
type memEntry struct {
	isDir   bool
	data    []byte
	mode    fs.FileMode
	modTime time.Time
}

type MemFS struct {
	files map[string]*memEntry // normalized slash paths
}

// ensureDir ensures directory entries exist for the given path
func (m *MemFS) ensureDir(path string) {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	if len(parts) == 0 {
		return
	}
	var curr string
	for i := 0; i < len(parts); i++ {
		if parts[i] == "." || parts[i] == "" {
			continue
		}
		if curr == "" {
			curr = parts[i]
		} else {
			curr = curr + "/" + parts[i]
		}
		if _, exists := m.files[curr]; !exists {
			m.files[curr] = &memEntry{isDir: true, mode: 0o755}
		}
	}
}

type memFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	isDir   bool
}

func (fi memFileInfo) Name() string       { return filepath.Base(fi.name) }
func (fi memFileInfo) Size() int64        { return fi.size }
func (fi memFileInfo) Mode() fs.FileMode  { return fi.mode }
func (fi memFileInfo) ModTime() time.Time { return fi.modTime }
func (fi memFileInfo) IsDir() bool        { return fi.isDir }
func (fi memFileInfo) Sys() any           { return nil }

type memOpenFile struct {
	entry *memEntry
	off   int64
	name  string
}

func (f *memOpenFile) Stat() (fs.FileInfo, error) {
	return memFileInfo{
		name:    f.name,
		size:    int64(len(f.entry.data)),
		mode:    f.entry.mode,
		modTime: f.entry.modTime,
		isDir:   f.entry.isDir,
	}, nil
}

func (f *memOpenFile) Read(p []byte) (int, error) {
	if f.entry.isDir {
		return 0, io.EOF
	}
	if f.off >= int64(len(f.entry.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.entry.data[f.off:])
	f.off += int64(n)
	return n, nil
}

func (f *memOpenFile) Close() error { return nil }

func (m *MemFS) Open(name string) (fs.File, error) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(name, "/")))
	if clean == "." {
		clean = ""
	}
	if clean == "" {
		// root
		return &memOpenFile{entry: &memEntry{isDir: true, mode: 0o755}, name: ""}, nil
	}
	if e, ok := m.files[clean]; ok {
		return &memOpenFile{entry: e, name: clean}, nil
	}
	return nil, fs.ErrNotExist
}

// Stat implements fs.StatFS
func (m *MemFS) Stat(name string) (fs.FileInfo, error) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(name, "/")))
	if clean == "." {
		clean = ""
	}
	if clean == "" {
		// root directory
		return memFileInfo{name: "", isDir: true, mode: 0o755}, nil
	}
	if e, ok := m.files[clean]; ok {
		return memFileInfo{
			name:    clean,
			size:    int64(len(e.data)),
			mode:    e.mode,
			modTime: e.modTime,
			isDir:   e.isDir,
		}, nil
	}
	return nil, fs.ErrNotExist
}

// NewMemFSFromTarGz builds a MemFS from a tar.gz stream.
func NewMemFSFromTarGz(r io.Reader) (*MemFS, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("invalid gzip stream: %w", err)
	}
	defer func() { _ = gzr.Close() }()
	tr := tar.NewReader(gzr)
	m := &MemFS{files: make(map[string]*memEntry)}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar read error: %w", err)
		}
		clean := filepath.ToSlash(filepath.Clean(hdr.Name))
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return nil, fmt.Errorf("invalid path in archive: %s", clean)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			m.ensureDir(clean)
			m.files[clean] = &memEntry{isDir: true, mode: fs.FileMode(hdr.Mode), modTime: hdr.ModTime}
		case tar.TypeReg:
			// Ensure dirs
			m.ensureDir(filepath.Dir(clean))
			data := make([]byte, hdr.Size)
			if _, err := io.ReadFull(tr, data); err != nil && err != io.ErrUnexpectedEOF {
				return nil, fmt.Errorf("failed to read file %s: %w", clean, err)
			}
			m.files[clean] = &memEntry{isDir: false, data: data, mode: fs.FileMode(hdr.Mode), modTime: hdr.ModTime}
		default:
			// skip other types
		}
	}
	return m, nil
}
