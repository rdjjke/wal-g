package storage

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
)

//go:generate mockery --name Folder
//go:generate mockgen -destination=../../../test/mocks/mock_folder.go -package mocks -build_flags -mod=readonly github.com/wal-g/wal-g/pkg/storages/storage Folder

type Folder interface {
	// GetPath provides a relative path from the root of the storage. It must always end with '/'.
	GetPath() string

	// ListFolder lists the folder and provides nested objects and folders. Objects must be with relative paths.
	// If the folder doesn't exist, empty objects and subFolders must be returned without any error.
	ListFolder() (objects []Object, subFolders []Folder, err error)

	// DeleteObjects deletes objects from the storage if they exist.
	DeleteObjects(objectRelativePaths []string) error

	// Exists checks if an object exists in the folder.
	Exists(objectRelativePath string) (bool, error)

	// GetSubFolder returns a handle to the subfolder. Does not have to instantiate the subfolder in any material form.
	GetSubFolder(subFolderRelativePath string) Folder

	// ReadObject reads an object from the folder. Must return ObjectNotFoundError in case the object doesn't exist.
	ReadObject(objectRelativePath string) (io.ReadCloser, error)

	// PutObject uploads a new object into the folder by a relative path. If an object with the same name already
	// exists, it is overwritten. Please prefer using PutObjectWithContext.
	PutObject(name string, content io.Reader) error

	// PutObjectWithContext uploads a new object into the folder by a relative path. If an object with the same name
	// already exists, it is overwritten. Operation can be terminated using Context.
	PutObjectWithContext(ctx context.Context, name string, content io.Reader) error

	// CopyObject copies an object from one place inside the folder to the other. Both paths must be relative. This is
	// an error if the source object doesn't exist.
	CopyObject(srcPath string, dstPath string) error
}

func ListFolderRecursively(folder Folder) (relativePathObjects []Object, err error) {
	return ListFolderRecursivelyWithFilter(folder, func(string) bool { return true })
}

func ListFolderRecursivelyWithFilter(
	folder Folder,
	folderSelector func(path string) bool,
) (relativePathObjects []Object, err error) {
	queue := make([]Folder, 0)
	queue = append(queue, folder)
	for len(queue) > 0 {
		subFolder := queue[0]
		queue = queue[1:]
		objects, subFolders, err := subFolder.ListFolder()
		folderPrefix := strings.TrimPrefix(subFolder.GetPath(), folder.GetPath())
		relativePathObjects = append(relativePathObjects, prependPaths(objects, folderPrefix)...)
		if err != nil {
			return nil, err
		}

		queue = append(queue, filterSubfolders(folder.GetPath(), subFolders, folderSelector)...)
	}
	return relativePathObjects, nil
}

func prependPaths(objects []Object, folderPrefix string) []Object {
	relativePathObjects := make([]Object, len(objects))
	for i, object := range objects {
		relativePathObjects[i] = NewLocalObject(
			path.Join(folderPrefix, object.GetName()),
			object.GetLastModified(),
			object.GetSize(),
		)
	}
	return relativePathObjects
}

// filterSubfolders returns subfolders matching the provided path selector
func filterSubfolders(rootFolderPath string, folders []Folder, selector func(path string) bool) []Folder {
	result := make([]Folder, 0)
	for i := range folders {
		folderPath := strings.TrimPrefix(folders[i].GetPath(), rootFolderPath)
		if selector(folderPath) {
			result = append(result, folders[i])
		}
	}
	return result
}

func ListFolderRecursivelyWithPrefix(folder Folder, prefix string) (relativePathObjects []Object, err error) {
	checkFile := len(prefix) > 0 && !strings.HasSuffix(prefix, "/")
	prefix = strings.Trim(prefix, "/")

	if checkFile {
		dirName, fileName := path.Split(prefix)
		parentFolder := folder.GetSubFolder(dirName)
		objects, _, err := parentFolder.ListFolder()
		if err != nil {
			return nil, fmt.Errorf("can't list folder %q: %w", dirName, err)
		}
		for _, obj := range objects {
			if obj.GetName() == fileName {
				return prependPaths([]Object{obj}, dirName), nil
			}
		}
	}

	parentFolder := folder.GetSubFolder(prefix)
	objects, err := ListFolderRecursively(parentFolder)
	if err != nil {
		return nil, fmt.Errorf("can't list folder %q: %w", prefix, err)
	}
	return prependPaths(objects, prefix), nil
}
