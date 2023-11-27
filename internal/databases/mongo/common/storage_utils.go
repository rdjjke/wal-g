package common

import (
	"github.com/wal-g/wal-g/internal"
	"github.com/wal-g/wal-g/internal/databases/mongo/models"
	"github.com/wal-g/wal-g/pkg/storages/storage"
	"github.com/wal-g/wal-g/utility"
)

const LogicalBackupType = "logical"
const BinaryBackupType = "binary"

func DownloadSentinel(folder storage.Folder, backupName string) (*models.Backup, error) {
	var sentinel models.Backup
	backup, err := internal.GetBackupByName(backupName, "", folder)
	if err != nil {
		return nil, err
	}
	if err := backup.FetchSentinel(&sentinel); err != nil {
		return nil, err
	}
	if sentinel.BackupName == "" {
		sentinel.BackupName = backupName
	}
	if sentinel.BackupType == "" {
		sentinel.BackupType = LogicalBackupType
	}
	return &sentinel, nil
}

func GetBackupFolder() (backupFolder storage.Folder, err error) {
	st, err := internal.ConfigureStorage()
	if err != nil {
		return nil, err
	}
	return st.RootFolder().GetSubFolder(utility.BaseBackupPath), err
}
