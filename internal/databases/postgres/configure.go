package postgres

import (
	"fmt"
	"io"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/viper"
	"github.com/wal-g/tracelog"
	"github.com/wal-g/wal-g/internal"
	"github.com/wal-g/wal-g/internal/fsutil"
	"github.com/wal-g/wal-g/internal/multistorage"
	"github.com/wal-g/wal-g/utility"
)

// ConfigureMultiStorage configures the primary storage and all failover ones, if any, and builds a multi-storage that
// aggregates them. It also sets up a cache to keep storage alive check results there.
// This function doesn't set any specific multi-storage root folder's policies, so policies.Default are used initially.
// checkWrite should be true for operations supposes writing to the storage. It affects selecting R/O or R/W aliveness check.
func ConfigureMultiStorage(checkWrite bool) (ms *multistorage.Storage, err error) {
	// errClosers are needed to close already configured storages if a fatal error happens before they are delegated to multi-storage.
	var errClosers []io.Closer
	defer func() {
		if err == nil {
			return
		}
		for _, closer := range errClosers {
			utility.LoggedClose(closer, "Failed to close storage")
		}
	}()

	primary, err := internal.ConfigureStorage()
	if err != nil {
		return nil, fmt.Errorf("configure primary storage: %w", err)
	}
	errClosers = append(errClosers, primary)

	failovers, err := internal.ConfigureFailoverStorages()
	if err != nil {
		return nil, fmt.Errorf("configure failover storages: %w", err)
	}
	for _, fo := range failovers {
		errClosers = append(errClosers, fo)
	}

	config := &multistorage.Config{}

	aliveChecksDefault := len(failovers) > 0
	config.AliveChecks, err = internal.GetBoolSettingDefault(internal.PgFailoverStoragesCheck, aliveChecksDefault)
	if err != nil {
		return nil, fmt.Errorf("get failover storage check setting: %w", err)
	}

	if config.AliveChecks {
		config.StatusCacheTTL, err = internal.GetDurationSetting(internal.PgFailoverStorageCacheLifetime)
		if err != nil {
			return nil, fmt.Errorf("get failover storage cache lifetime setting: %w", err)
		}

		config.AliveCheckTimeout, err = internal.GetDurationSetting(internal.PgFailoverStoragesCheckTimeout)
		if err != nil {
			return nil, fmt.Errorf("get failover storage check timeout setting: %w", err)
		}

		config.CheckWrite = checkWrite

		if config.CheckWrite {
			config.AliveCheckWriteBytes = viper.GetSizeInBytes(internal.PgFailoverStoragesCheckSize)
		}
	}

	return multistorage.NewStorage(config, primary, failovers), nil
}

// ConfigureWalUploader connects to storage and creates an uploader. It makes sure
// that a valid session has started; if invalid, returns AWS error
// and `<nil>` values.
func ConfigureWalUploader(baseUploader internal.Uploader) (uploader *WalUploader, err error) {
	useWalDelta, deltaDataFolder, err := configureWalDeltaUsage()
	if err != nil {
		return nil, errors.Wrap(err, "failed to configure WAL Delta usage")
	}

	var deltaFileManager *DeltaFileManager
	if useWalDelta {
		deltaFileManager = NewDeltaFileManager(deltaDataFolder)
	}

	uploader = NewWalUploader(baseUploader, deltaFileManager)
	return uploader, err
}

// TODO : unit tests
func configureWalDeltaUsage() (useWalDelta bool, deltaDataFolder fsutil.DataFolder, err error) {
	useWalDelta = viper.GetBool(internal.UseWalDeltaSetting)
	if !useWalDelta {
		return
	}
	dataFolderPath := internal.GetDataFolderPath()
	deltaDataFolder, err = fsutil.NewDiskDataFolder(dataFolderPath)
	if err != nil {
		useWalDelta = false
		tracelog.WarningLogger.Printf("can't use wal delta feature because can't open delta data folder '%s'"+
			" due to error: '%v'\n", dataFolderPath, err)
		err = nil
	}
	return
}

func getStopBackupTimeoutSetting() (time.Duration, error) {
	if !viper.IsSet(internal.PgStopBackupTimeout) {
		return 0, nil
	}

	timeout, err := internal.GetDurationSetting(internal.PgStopBackupTimeout)
	if err != nil {
		return 0, err
	}

	return timeout, nil
}
