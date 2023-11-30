package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/wal-g/wal-g/internal/crypto/yckms"

	"github.com/pkg/errors"
	"github.com/spf13/viper"
	"github.com/wal-g/tracelog"
	"github.com/wal-g/wal-g/internal/compression"
	"github.com/wal-g/wal-g/internal/crypto"
	"github.com/wal-g/wal-g/internal/crypto/awskms"
	cachenvlpr "github.com/wal-g/wal-g/internal/crypto/envelope/enveloper/cached"
	yckmsenvlpr "github.com/wal-g/wal-g/internal/crypto/envelope/enveloper/yckms"
	envopenpgp "github.com/wal-g/wal-g/internal/crypto/envelope/openpgp"
	"github.com/wal-g/wal-g/internal/crypto/openpgp"
	"github.com/wal-g/wal-g/internal/fsutil"
	"github.com/wal-g/wal-g/internal/limiters"
	"github.com/wal-g/wal-g/pkg/storages/storage"
	"golang.org/x/time/rate"
)

const (
	pgDefaultDatabasePageSize = 8192
	DefaultDataBurstRateLimit = 8 * pgDefaultDatabasePageSize
	DefaultDataFolderPath     = "/tmp"
	WaleFileHost              = "file://localhost"
)

const MinAllowedConcurrency = 1

var DeprecatedExternalGpgMessage = fmt.Sprintf(
	`You are using deprecated functionality that uses an external gpg library.
It will be removed in next major version.
Please set GPG key using environment variables %s or %s.
`, PgpKeySetting, PgpKeyPathSetting)

type UnconfiguredStorageError struct {
	error
}

func newUnconfiguredStorageError(storagePrefixVariants []string) UnconfiguredStorageError {
	return UnconfiguredStorageError{
		errors.Errorf("No storage is configured now, please set one of following settings: %v",
			storagePrefixVariants)}
}

func (err UnconfiguredStorageError) Error() string {
	return fmt.Sprintf(tracelog.GetErrorFormatter(), err.error)
}

type UnknownCompressionMethodError struct {
	error
}

func newUnknownCompressionMethodError(method string) UnknownCompressionMethodError {
	return UnknownCompressionMethodError{
		errors.Errorf("Unknown compression method: '%s', supported methods are: %v",
			method, compression.CompressingAlgorithms)}
}

func (err UnknownCompressionMethodError) Error() string {
	return fmt.Sprintf(tracelog.GetErrorFormatter(), err.error)
}

type UnsetRequiredSettingError struct {
	error
}

func NewUnsetRequiredSettingError(settingName string) UnsetRequiredSettingError {
	return UnsetRequiredSettingError{errors.Errorf("%v is required to be set, but it isn't", settingName)}
}

func (err UnsetRequiredSettingError) Error() string {
	return fmt.Sprintf(tracelog.GetErrorFormatter(), err.error)
}

type InvalidConcurrencyValueError struct {
	error
}

func newInvalidConcurrencyValueError(concurrencyType string, value int) InvalidConcurrencyValueError {
	return InvalidConcurrencyValueError{
		errors.Errorf("%v value is expected to be positive but is: %v",
			concurrencyType, value)}
}

func (err InvalidConcurrencyValueError) Error() string {
	return fmt.Sprintf(tracelog.GetErrorFormatter(), err.error)
}

type UnmarshallingError struct {
	error
}

func newUnmarshallingError(subject string, err error) UnmarshallingError {
	return UnmarshallingError{errors.Errorf("Failed to unmarshal %s: %v", subject, err)}
}

func (err UnmarshallingError) Error() string {
	return fmt.Sprintf(tracelog.GetErrorFormatter(), err.error)
}

// TODO : unit tests
func ConfigureLimiters() {
	if Turbo {
		return
	}
	if viper.IsSet(DiskRateLimitSetting) {
		diskLimit := viper.GetInt64(DiskRateLimitSetting)
		limiters.DiskLimiter = rate.NewLimiter(rate.Limit(diskLimit),
			int(diskLimit+DefaultDataBurstRateLimit)) // Add 8 pages to possible bursts
	}

	if viper.IsSet(NetworkRateLimitSetting) {
		netLimit := viper.GetInt64(NetworkRateLimitSetting)
		limiters.NetworkLimiter = rate.NewLimiter(rate.Limit(netLimit),
			int(netLimit+DefaultDataBurstRateLimit)) // Add 8 pages to possible bursts
	}
}

// TODO : unit tests
func ConfigureStorage() (storage.HashableStorage, error) {
	var rootWraps []storage.WrapRootFolder
	if limiters.NetworkLimiter != nil {
		rootWraps = append(rootWraps, func(prevFolder storage.Folder) (newFolder storage.Folder) {
			return NewLimitedFolder(prevFolder, limiters.NetworkLimiter)
		})
	}
	rootWraps = append(rootWraps, ConfigureStoragePrefix)

	st, err := ConfigureStorageForSpecificConfig(viper.GetViper(), rootWraps...)
	if err != nil {
		return nil, err
	}

	return st, nil
}

func ConfigureStoragePrefix(folder storage.Folder) storage.Folder {
	prefix := viper.GetString(StoragePrefixSetting)
	if prefix != "" {
		folder = folder.GetSubFolder(prefix)
	}
	return folder
}

// TODO: something with that
// when provided multiple 'keys' in the config,
// this function will always return only one concrete 'storage'.
// Chosen folder depends only on 'StorageAdapters' order
func ConfigureStorageForSpecificConfig(
	config *viper.Viper,
	rootWraps ...storage.WrapRootFolder,
) (storage.HashableStorage, error) {
	skippedPrefixes := make([]string, 0)
	for _, adapter := range StorageAdapters {
		prefix, ok := getWaleCompatibleSettingFrom(adapter.PrefixSettingKey(), config)
		if !ok {
			skippedPrefixes = append(skippedPrefixes, "WALG_"+adapter.PrefixSettingKey())
			continue
		}

		settings := adapter.loadSettings(config)
		st, err := adapter.configure(prefix, settings, rootWraps...)
		if err != nil {
			return nil, fmt.Errorf("configure storage with prefix %q: %w", prefix, err)
		}
		return st, nil
	}
	return nil, newUnconfiguredStorageError(skippedPrefixes)
}

func getWalFolderPath() string {
	if !viper.IsSet(PgDataSetting) {
		return DefaultDataFolderPath
	}
	return getRelativeWalFolderPath(viper.GetString(PgDataSetting))
}

func getRelativeWalFolderPath(pgdata string) string {
	for _, walDir := range []string{"pg_wal", "pg_xlog"} {
		dataFolderPath := filepath.Join(pgdata, walDir)
		if _, err := os.Stat(dataFolderPath); err == nil {
			return dataFolderPath
		}
	}
	return DefaultDataFolderPath
}

func GetDataFolderPath() string {
	return filepath.Join(getWalFolderPath(), "walg_data")
}

// GetPgSlotName reads the slot name from the environment
func GetPgSlotName() (pgSlotName string) {
	pgSlotName = viper.GetString(PgSlotName)
	if pgSlotName == "" {
		pgSlotName = "walg"
	}
	return
}

func ConfigureCompressor() (compression.Compressor, error) {
	compressionMethod := viper.GetString(CompressionMethodSetting)
	if _, ok := compression.Compressors[compressionMethod]; !ok {
		return nil, newUnknownCompressionMethodError(compressionMethod)
	}
	return compression.Compressors[compressionMethod], nil
}

func ConfigureLogging() error {
	if viper.IsSet(LogLevelSetting) {
		return tracelog.UpdateLogLevel(viper.GetString(LogLevelSetting))
	}
	return nil
}

func getPGArchiveStatusFolderPath() string {
	return filepath.Join(getWalFolderPath(), "archive_status")
}

func getArchiveDataFolderPath() string {
	return filepath.Join(GetDataFolderPath(), "walg_archive_status")
}

func GetRelativeArchiveDataFolderPath() string {
	return filepath.Join(getRelativeWalFolderPath(""), "walg_data", "walg_archive_status")
}

// TODO : unit tests
func ConfigureArchiveStatusManager() (fsutil.DataFolder, error) {
	return fsutil.NewDiskDataFolder(getArchiveDataFolderPath())
}

func ConfigurePGArchiveStatusManager() (fsutil.DataFolder, error) {
	return fsutil.ExistingDiskDataFolder(getPGArchiveStatusFolderPath())
}

// ConfigureUploader is like ConfigureUploaderToFolder, but configures the default storage.
func ConfigureUploader() (*RegularUploader, error) {
	st, err := ConfigureStorage()
	if err != nil {
		return nil, errors.Wrap(err, "failed to configure storage")
	}

	uploader, err := ConfigureUploaderToFolder(st.RootFolder())
	return uploader, err
}

// ConfigureUploaderToFolder connects to storage with the specified folder and creates an uploader.
// It makes sure that a valid session has started; if invalid, returns AWS error and `<nil>` value.
func ConfigureUploaderToFolder(folder storage.Folder) (uploader *RegularUploader, err error) {
	compressor, err := ConfigureCompressor()
	if err != nil {
		return nil, errors.Wrap(err, "failed to configure compression")
	}

	uploader = NewRegularUploader(compressor, folder)
	return uploader, err
}

func ConfigureUploaderWithoutCompressor() (Uploader, error) {
	st, err := ConfigureStorage()
	if err != nil {
		return nil, errors.Wrap(err, "failed to configure storage")
	}

	uploader := NewRegularUploader(nil, st.RootFolder())
	return uploader, err
}

func ConfigureSplitUploader() (Uploader, error) {
	uploader, err := ConfigureUploader()
	if err != nil {
		return nil, err
	}

	var partitions = viper.GetInt(StreamSplitterPartitions)
	var blockSize = viper.GetSizeInBytes(StreamSplitterBlockSize)
	var maxFileSize = viper.GetInt(StreamSplitterMaxFileSize)

	splitStreamUploader := NewSplitStreamUploader(uploader, partitions, int(blockSize), maxFileSize)
	return splitStreamUploader, nil
}

func ConfigureCrypter() crypto.Crypter {
	crypter, err := ConfigureCrypterForSpecificConfig(viper.GetViper())
	if err != nil {
		tracelog.ErrorLogger.FatalfOnError("can't configure crypter: %v", err)
	}
	return crypter
}

func CrypterFromConfig(configFile string) crypto.Crypter {
	var config = viper.New()
	SetDefaultValues(config)
	ReadConfigFromFile(config, configFile)
	CheckAllowedSettings(config)

	crypter, err := ConfigureCrypterForSpecificConfig(config)
	if err != nil {
		tracelog.ErrorLogger.FatalfOnError("can't configure crypter: %v", err)
	}
	return crypter
}

// ConfigureCrypter uses environment variables to create and configure a crypter.
// In case no configuration in environment variables found, return `<nil>` crypter.
func ConfigureCrypterForSpecificConfig(config *viper.Viper) (crypto.Crypter, error) {
	pgpKey := config.IsSet(PgpKeySetting)
	pgpKeyPath := config.IsSet(PgpKeyPathSetting)
	legacyGpg := config.IsSet(GpgKeyIDSetting)

	envelopePgpKey := config.IsSet(PgpEnvelopKeyPathSetting)
	envelopePgpKeyPath := config.IsSet(PgpEnvelopeKeySetting)

	libsodiumKey := config.IsSet(LibsodiumKeySetting)
	libsodiumKeyPath := config.IsSet(LibsodiumKeySetting)

	isPgpKey := pgpKey || pgpKeyPath || legacyGpg
	isEnvelopePgpKey := envelopePgpKey || envelopePgpKeyPath
	isLibsodium := libsodiumKey || libsodiumKeyPath

	if isPgpKey && isEnvelopePgpKey {
		return nil, errors.New("there is no way to configure plain gpg and envelope gpg at the same time, please choose one")
	}

	switch {
	case isPgpKey:
		return configurePgpCrypter(config)
	case isEnvelopePgpKey:
		return configureEnvelopePgpCrypter(config)
	case config.IsSet(CseKmsIDSetting):
		return awskms.CrypterFromKeyID(config.GetString(CseKmsIDSetting), config.GetString(CseKmsRegionSetting)), nil
	case config.IsSet(YcKmsKeyIDSetting):
		return yckms.YcCrypterFromKeyIDAndCredential(config.GetString(YcKmsKeyIDSetting), config.GetString(YcSaKeyFileSetting)), nil
	case isLibsodium:
		return configureLibsodiumCrypter(config)
	default:
		return nil, nil
	}
}

func configurePgpCrypter(config *viper.Viper) (crypto.Crypter, error) {
	loadPassphrase := func() (string, bool) {
		return GetSetting(PgpKeyPassphraseSetting)
	}
	// key can be either private (for download) or public (for upload)
	if config.IsSet(PgpKeySetting) {
		return openpgp.CrypterFromKey(config.GetString(PgpKeySetting), loadPassphrase), nil
	}

	// key can be either private (for download) or public (for upload)
	if config.IsSet(PgpKeyPathSetting) {
		return openpgp.CrypterFromKeyPath(config.GetString(PgpKeyPathSetting), loadPassphrase), nil
	}

	if keyRingID, ok := getWaleCompatibleSetting(GpgKeyIDSetting); ok {
		tracelog.WarningLogger.Printf(DeprecatedExternalGpgMessage)
		return openpgp.CrypterFromKeyRingID(keyRingID, loadPassphrase), nil
	}
	return nil, errors.New("there is no any supported gpg crypter configuration")
}

func configureEnvelopePgpCrypter(config *viper.Viper) (crypto.Crypter, error) {
	if !config.IsSet(PgpEnvelopeYcKmsKeyIDSetting) {
		return nil, errors.New("yandex cloud KMS key for client-side encryption and decryption must be configured")
	}

	yckmsEnveloper, err := yckmsenvlpr.EnveloperFromKeyIDAndCredential(
		config.GetString(PgpEnvelopeYcKmsKeyIDSetting),
		config.GetString(PgpEnvelopeYcSaKeyFileSetting),
		config.GetString(PgpEnvelopeYcEndpointSetting),
	)
	if err != nil {
		return nil, err
	}
	expiration, err := GetDurationSetting(PgpEnvelopeCacheExpiration)
	if err != nil {
		return nil, err
	}
	enveloper := cachenvlpr.EnveloperWithCache(yckmsEnveloper, expiration)

	if config.IsSet(PgpEnvelopKeyPathSetting) {
		return envopenpgp.CrypterFromKeyPath(viper.GetString(PgpEnvelopKeyPathSetting), enveloper), nil
	}
	if config.IsSet(PgpEnvelopeKeySetting) {
		return envopenpgp.CrypterFromKey(viper.GetString(PgpEnvelopeKeySetting), enveloper), nil
	}
	return nil, errors.New("there is no any supported envelope gpg crypter configuration")
}

func GetMaxDownloadConcurrency() (int, error) {
	return GetMaxConcurrency(DownloadConcurrencySetting)
}

func GetMaxUploadConcurrency() (int, error) {
	return GetMaxConcurrency(UploadConcurrencySetting)
}

// This setting is intentionally undocumented in README. Effectively, this configures how many prepared tar Files there
// may be in uploading state during backup-push.
func getMaxUploadQueue() (int, error) {
	return GetMaxConcurrency(UploadQueueSetting)
}

// TODO : unit tests
func GetDeltaConfig() (maxDeltas int, fromFull bool) {
	maxDeltas = viper.GetInt(DeltaMaxStepsSetting)
	if origin, hasOrigin := GetSetting(DeltaOriginSetting); hasOrigin {
		switch origin {
		case LatestString:
		case "LATEST_FULL":
			fromFull = true
		default:
			tracelog.ErrorLogger.Fatalf("Unknown %s: %s\n", DeltaOriginSetting, origin)
		}
	}
	return
}

func GetMaxUploadDiskConcurrency() (int, error) {
	if Turbo {
		return 4, nil
	}
	return GetMaxConcurrency(UploadDiskConcurrencySetting)
}

func GetMaxConcurrency(concurrencyType string) (int, error) {
	concurrency := viper.GetInt(concurrencyType)

	if concurrency < MinAllowedConcurrency {
		return MinAllowedConcurrency, newInvalidConcurrencyValueError(concurrencyType, concurrency)
	}
	return concurrency, nil
}

func GetSentinelUserData() (interface{}, error) {
	dataStr, ok := GetSetting(SentinelUserDataSetting)
	if !ok {
		return nil, nil
	}
	return UnmarshalSentinelUserData(dataStr)
}

func UnmarshalSentinelUserData(userDataStr string) (interface{}, error) {
	if len(userDataStr) == 0 {
		return nil, nil
	}

	var out interface{}
	err := json.Unmarshal([]byte(userDataStr), &out)
	if err != nil {
		return nil, errors.Wrapf(newUnmarshallingError(userDataStr, err), "failed to read the user data as a JSON object")
	}
	return out, nil
}

func GetCommandSettingContext(ctx context.Context, variableName string) (*exec.Cmd, error) {
	dataStr, ok := GetSetting(variableName)
	if !ok {
		tracelog.InfoLogger.Printf("command %s not configured", variableName)
		return nil, errors.New("command not configured")
	}
	if len(dataStr) == 0 {
		tracelog.ErrorLogger.Print(variableName + " expected.")
		return nil, errors.New(variableName + " not configured")
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell, "-c", dataStr)
	// do not shut up subcommands by default
	cmd.Stderr = os.Stderr
	return cmd, nil
}

func GetCommandSetting(variableName string) (*exec.Cmd, error) {
	return GetCommandSettingContext(context.Background(), variableName)
}

func GetOplogArchiveAfterSize() (int, error) {
	oplogArchiveAfterSizeStr, _ := GetSetting(OplogArchiveAfterSize)
	oplogArchiveAfterSize, err := strconv.Atoi(oplogArchiveAfterSizeStr)
	if err != nil {
		return 0,
			fmt.Errorf("integer expected for %s setting but given '%s': %w",
				OplogArchiveAfterSize, oplogArchiveAfterSizeStr, err)
	}
	return oplogArchiveAfterSize, nil
}

func GetDurationSetting(setting string) (time.Duration, error) {
	intervalStr, ok := GetSetting(setting)
	if !ok {
		return 0, NewUnsetRequiredSettingError(setting)
	}
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return 0, fmt.Errorf("duration expected for %s setting but given '%s': %w", setting, intervalStr, err)
	}
	return interval, nil
}

func GetOplogPITRDiscoveryIntervalSetting() (*time.Duration, error) {
	durStr, ok := GetSetting(OplogPITRDiscoveryInterval)
	if !ok {
		return nil, nil
	}
	dur, err := time.ParseDuration(durStr)
	if err != nil {
		return nil, fmt.Errorf("duration expected for %s setting but given '%s': %w", OplogPITRDiscoveryInterval, durStr, err)
	}
	return &dur, nil
}

func GetRequiredSetting(setting string) (string, error) {
	val, ok := GetSetting(setting)
	if !ok {
		return "", NewUnsetRequiredSettingError(setting)
	}
	return val, nil
}

func GetBoolSettingDefault(setting string, def bool) (bool, error) {
	val, ok := GetSetting(setting)
	if !ok {
		return def, nil
	}
	return strconv.ParseBool(val)
}

func GetBoolSetting(setting string) (val bool, ok bool, err error) {
	valstr, ok := GetSetting(setting)
	if !ok {
		return false, false, nil
	}
	val, err = strconv.ParseBool(valstr)
	return val, true, err
}
