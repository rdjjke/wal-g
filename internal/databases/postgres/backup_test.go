package postgres_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wal-g/wal-g/internal/databases/postgres"

	"github.com/stretchr/testify/assert"
	"github.com/wal-g/wal-g/internal"
	"github.com/wal-g/wal-g/testtools"
	"github.com/wal-g/wal-g/utility"
)

func getMockBackupFromFiles(files internal.BackupFileList) postgres.Backup {
	return postgres.Backup{
		SentinelDto:      &postgres.BackupSentinelDto{},
		FilesMetadataDto: &postgres.FilesMetadataDto{Files: files},
	}
}

func TestGetFilesToUnwrap_SimpleFile(t *testing.T) {
	backup := getMockBackupFromFiles(testtools.NewBackupFileListBuilder().WithSimple().Build())

	files, _ := backup.GetFilesToUnwrap("")
	assert.Contains(t, files, testtools.SimplePath)
}

func TestGetFilesToUnwrap_IncrementedFile(t *testing.T) {
	backup := getMockBackupFromFiles(testtools.NewBackupFileListBuilder().WithIncremented().Build())

	files, _ := backup.GetFilesToUnwrap("")
	assert.Contains(t, files, testtools.IncrementedPath)
}

func TestGetFilesToUnwrap_SkippedFile(t *testing.T) {
	backup := getMockBackupFromFiles(testtools.NewBackupFileListBuilder().WithSkipped().Build())

	files, _ := backup.GetFilesToUnwrap("")
	assert.Contains(t, files, testtools.SkippedPath)
}

func TestGetFilesToUnwrap_UnwrapAll(t *testing.T) {
	backup := getMockBackupFromFiles(testtools.NewBackupFileListBuilder().Build())

	files, _ := backup.GetFilesToUnwrap("")
	assert.True(t, files == nil)
}

func TestGetFilesToUnwrap_NoMoreFiles(t *testing.T) {
	backup := getMockBackupFromFiles(testtools.NewBackupFileListBuilder().
		WithSimple().
		WithIncremented().
		WithSkipped().
		Build())

	files, _ := backup.GetFilesToUnwrap("")
	expected := map[string]bool{
		testtools.SimplePath:      true,
		testtools.IncrementedPath: true,
		testtools.SkippedPath:     true,
	}
	for utilityPath := range postgres.UtilityFilePaths {
		expected[utilityPath] = true
	}
	assert.Equal(t, expected, files)
}

func TestCheckExistenceWhenBackupExists(t *testing.T) {
	folder := testtools.CreateMockStorageFolder()
	backup, err := postgres.NewBackup(folder.GetSubFolder(utility.BaseBackupPath), "base_000")
	require.NoError(t, err)
	exists, err := backup.CheckExistence()
	assert.NoError(t, err)
	assert.True(t, exists)
}

func TestCheckExistenceWhenBackupNotExists(t *testing.T) {
	folder := testtools.CreateMockStorageFolder()
	backup, err := postgres.NewBackup(folder.GetSubFolder(utility.BaseBackupPath), "base_321")
	require.NoError(t, err)
	exists, err := backup.CheckExistence()
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestGetTarNames(t *testing.T) {
	folder := testtools.CreateMockStorageFolder()
	backup, err := postgres.NewBackup(folder.GetSubFolder(utility.BaseBackupPath), "base_456")
	require.NoError(t, err)
	tarNames, err := backup.GetTarNames()
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"1", "2", "3"}, tarNames)
}

func TestIsPgControlRequired(t *testing.T) {
	folder := testtools.CreateMockStorageFolder()
	backup, err := postgres.NewBackup(folder.GetSubFolder(utility.BaseBackupPath), "base_456")
	require.NoError(t, err)
	_, err = backup.GetSentinel()
	assert.NoError(t, err)
	assert.True(t, postgres.IsPgControlRequired(backup))
}

func TestIsPgControlNotRequiredForWALEBackups(t *testing.T) {
	folder := testtools.CreateMockStorageFolder()
	backup, err := postgres.NewBackup(folder.GetSubFolder(utility.BaseBackupPath), "base_000000010000DD170000000C_00743784")
	require.NoError(t, err)
	backup.SentinelDto = &postgres.BackupSentinelDto{}
	assert.False(t, postgres.IsPgControlRequired(backup))
}

func TestFetchSentinel(t *testing.T) {
	folder := testtools.CreateMockStorageFolder()
	expectedSentinel := postgres.BackupSentinelDto{}
	expectedSentinelJson, _ := json.Marshal(expectedSentinel)
	_ = folder.PutObject("base_789454598_backup_stop_sentinel.json", bytes.NewReader(expectedSentinelJson))
	backup, err := postgres.NewBackup(folder, "base_789454598")
	require.NoError(t, err)

	actualSentinel, err := backup.GetSentinel()

	assert.NoError(t, err)
	assert.Equal(t, expectedSentinel, actualSentinel)
}

func TestFetchSentinelReturnErrorWhenSentinelNotExist(t *testing.T) {
	folder := testtools.CreateMockStorageFolder()
	backup, err := postgres.NewBackup(folder.GetSubFolder(utility.BaseBackupPath), "base_78934085033849")
	require.NoError(t, err)
	_, err = backup.GetSentinel()

	assert.Error(t, err)
}

func TestFetchSentinelReturnErrorWhenSentinelUnmarshallable(t *testing.T) {
	folder := testtools.CreateMockStorageFolder()
	backupName := "base_000"
	backup, err := postgres.NewBackup(folder.GetSubFolder(utility.BaseBackupPath), backupName)
	require.NoError(t, err)
	errorMessage := fmt.Sprintf("failed to fetch dto from %s", backupName+utility.SentinelSuffix)

	_, err = backup.GetSentinel()

	assert.Error(t, err)
	assert.Equal(t, errorMessage, err.Error()[:len(errorMessage)])
}

func TestGetLatestBackupName(t *testing.T) {
	var folder = testtools.MakeDefaultInMemoryStorageFolder()
	backupNames := []string{"base_123", "base_456", "base000"}
	for _, nameBackupPrefix := range backupNames {
		nameBackup := nameBackupPrefix + utility.SentinelSuffix
		_ = folder.PutObject(nameBackup, &bytes.Buffer{})

		latestBackup, err := internal.GetLatestBackup(folder)
		assert.NoError(t, err)
		assert.Equal(t, nameBackupPrefix, latestBackup.Name)
	}
}

func TestGetLatestBackupNameNoBackupsInFolder(t *testing.T) {
	folder := testtools.MakeDefaultInMemoryStorageFolder()
	baseBackupFolder := folder.GetSubFolder(utility.BaseBackupPath)
	backup, err := internal.GetLatestBackup(baseBackupFolder)

	assert.Error(t, err, internal.NoBackupsFoundError{})
	assert.Equal(t, backup.Name, "")
}

func TestGetLastBackupNameWithGarbage(t *testing.T) {
	folder := testtools.CreateMockStorageFolder()
	subFolder := folder.GetSubFolder(utility.BaseBackupPath)
	latestBackup, err := internal.GetLatestBackup(subFolder)

	assert.NoError(t, err)
	assert.Equal(t, "base_000", latestBackup.Name)
}
