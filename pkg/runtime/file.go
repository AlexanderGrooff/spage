package runtime

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func WriteLocalFile(filename string, data string) error {
	return os.WriteFile(filename, []byte(data), 0644)
}

func WriteRemoteFile(host, remotePath, data, username string) error {
	tmpFile, err := os.CreateTemp("", "tempfile")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(data)); err != nil {
		return fmt.Errorf("failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %v", err)
	}

	_, stderr, err := RunLocalCommand(fmt.Sprintf("scp %q %s:%s", tmpFile.Name(), host, tmpFile.Name()), "")
	if err != nil {
		return fmt.Errorf("failed to transfer file to remote host: %v, %s", err, stderr)
	}
	_, _, err = RunRemoteCommand(host, fmt.Sprintf("mv %s %s", tmpFile.Name(), remotePath), username)
	if err != nil {
		return fmt.Errorf("failed to move file to final location: %v", err)
	}

	return nil
}

func CopyLocal(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		return copyLocalDir(src, dst)
	}
	return copyLocalFile(src, dst)
}

func CopyRemote(src, dst string) error {
	_, _, err := RunRemoteCommand("cp -r %s %s", src, dst)
	return err
}

func copyLocalDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err = os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err = copyLocalDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err = copyLocalFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyLocalFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
