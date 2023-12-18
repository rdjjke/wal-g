package cache

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

type SharedFile struct {
	Path    string
	Updated time.Time
}

func NewSharedFile(path string) *SharedFile {
	return &SharedFile{
		Path:    path,
		Updated: time.Now(),
	}
}

func (sf *SharedFile) read() (storageStatuses, error) {
	file, err := os.OpenFile(sf.Path, os.O_RDONLY, 0666)
	if err != nil {
		return nil, fmt.Errorf("open cache file: %w", err)
	}
	defer func() { _ = file.Close() }()

	err = lockFile(file, false)
	if err != nil {
		return nil, fmt.Errorf("acquire shared lock for the cache file: %w", err)
	}

	bytes, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read cache file: %w", err)
	}

	var validJSONContent map[string]storageStatus
	err = json.Unmarshal(bytes, &validJSONContent)
	if err != nil {
		return nil, fmt.Errorf("unmarshal cache file content: %w", err)
	}
	content := make(storageStatuses, len(validJSONContent))
	for str, stat := range validJSONContent {
		content[ParseKey(str)] = stat
	}

	return content, nil
}

func (sf *SharedFile) write(content storageStatuses) error {
	validJSONContent := make(map[string]storageStatus, len(content))
	for key, stat := range content {
		validJSONContent[key.String()] = stat
	}
	bytes, err := json.Marshal(validJSONContent)
	if err != nil {
		return fmt.Errorf("marshal cache file content: %w", err)
	}

	file, err := os.OpenFile(sf.Path, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return fmt.Errorf("open cache file: %w", err)
	}
	defer func() { _ = file.Close() }()

	err = lockFile(file, true)
	if err != nil {
		return fmt.Errorf("acquire exclusive lock for the cache file: %w", err)
	}

	err = file.Truncate(int64(len(bytes)))
	if err != nil {
		return fmt.Errorf("truncate cache file: %w", err)
	}

	_, err = file.Write(bytes)
	if err != nil {
		return fmt.Errorf("write cache file: %w", err)
	}

	return nil
}

func lockFile(file *os.File, exclusive bool) (err error) {
	how := unix.LOCK_SH
	if exclusive {
		how = unix.LOCK_EX
	}

	for {
		err = unix.Flock(int(file.Fd()), how)
		// When calling syscalls directly, we need to retry EINTR errors. They mean the call was interrupted by a signal.
		if err != unix.EINTR {
			break
		}
	}
	return err
}
