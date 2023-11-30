package s3

import (
	"fmt"

	"github.com/wal-g/wal-g/pkg/storages/storage"
	"github.com/wal-g/wal-g/pkg/storages/storage/setting"
)

// TODO: Merge the settings and their default values with ones defined in internal/config.go

const (
	endpointSetting                 = "AWS_ENDPOINT"
	regionSetting                   = "AWS_REGION"
	forcePathStyleSetting           = "AWS_S3_FORCE_PATH_STYLE"
	accessKeyIDSetting              = "AWS_ACCESS_KEY_ID"
	accessKeySetting                = "AWS_ACCESS_KEY"
	secretAccessKeySetting          = "AWS_SECRET_ACCESS_KEY"
	secretKeySetting                = "AWS_SECRET_KEY"
	sessionTokenSetting             = "AWS_SESSION_TOKEN"
	sessionNameSetting              = "AWS_ROLE_SESSION_NAME"
	roleARNSetting                  = "AWS_ROLE_ARN"
	useYcSessionTokenSetting        = "S3_USE_YC_SESSION_TOKEN"
	sseSetting                      = "S3_SSE"
	sseCSetting                     = "S3_SSE_C"
	sseKmsIDSetting                 = "S3_SSE_KMS_ID"
	storageClassSetting             = "S3_STORAGE_CLASS"
	uploadConcurrencySetting        = "UPLOAD_CONCURRENCY"
	caCertFileSetting               = "S3_CA_CERT_FILE"
	maxPartSizeSetting              = "S3_MAX_PART_SIZE"
	endpointSourceSetting           = "S3_ENDPOINT_SOURCE"
	endpointPortSetting             = "S3_ENDPOINT_PORT"
	logLevelSetting                 = "S3_LOG_LEVEL"
	useListObjectsV1Setting         = "S3_USE_LIST_OBJECTS_V1"
	rangeBatchEnabledSetting        = "S3_RANGE_BATCH_ENABLED"
	rangeQueriesMaxRetriesSetting   = "S3_RANGE_MAX_RETRIES"
	requestAdditionalHeadersSetting = "S3_REQUEST_ADDITIONAL_HEADERS"
	// maxRetriesSetting limits retries during interaction with S3
	maxRetriesSetting = "S3_MAX_RETRIES"
)

var SettingList = []string{
	endpointPortSetting,
	endpointSetting,
	endpointSourceSetting,
	regionSetting,
	forcePathStyleSetting,
	accessKeyIDSetting,
	accessKeySetting,
	secretAccessKeySetting,
	secretKeySetting,
	sessionTokenSetting,
	sessionNameSetting,
	roleARNSetting,
	useYcSessionTokenSetting,
	sseSetting,
	sseCSetting,
	sseKmsIDSetting,
	storageClassSetting,
	uploadConcurrencySetting,
	caCertFileSetting,
	maxPartSizeSetting,
	useListObjectsV1Setting,
	logLevelSetting,
	rangeBatchEnabledSetting,
	rangeQueriesMaxRetriesSetting,
	maxRetriesSetting,
	requestAdditionalHeadersSetting,
}

const (
	defaultPort              = "443"
	defaultForcePathStyle    = false
	defaultUseListObjectsV1  = false
	defaultMaxRetries        = 15
	defaultMaxPartSize       = 20 << 20
	defaultStorageClass      = "STANDARD"
	defaultRangeBatchEnabled = false
	defaultRangeMaxRetries   = 10
)

// TODO: Unit tests
func ConfigureStorage(
	prefix string,
	settings map[string]string,
	rootWraps ...storage.WrapRootFolder,
) (storage.HashableStorage, error) {
	bucket, rootPath, err := storage.GetPathFromPrefix(prefix)
	if err != nil {
		return nil, fmt.Errorf("extract bucket and path from prefix %q: %w", prefix, err)
	}

	port := defaultPort
	if p, ok := settings[endpointPortSetting]; ok {
		port = p
	}
	forcePathStyle, err := setting.BoolOptional(settings, forcePathStyleSetting, defaultForcePathStyle)
	if err != nil {
		return nil, err
	}
	useListObjectsV1, err := setting.BoolOptional(settings, useListObjectsV1Setting, defaultUseListObjectsV1)
	if err != nil {
		return nil, err
	}
	maxRetries, err := setting.IntOptional(settings, maxRetriesSetting, defaultMaxRetries)
	if err != nil {
		return nil, err
	}
	uploadConcurrency, err := setting.Int(settings, uploadConcurrencySetting)
	if err != nil {
		return nil, err
	}
	maxPartSize, err := setting.IntOptional(settings, maxPartSizeSetting, defaultMaxPartSize)
	if err != nil {
		return nil, err
	}
	storageClass := defaultStorageClass
	if class, ok := settings[storageClassSetting]; ok {
		storageClass = class
	}
	rangeBatchEnabled, err := setting.BoolOptional(settings, rangeBatchEnabledSetting, defaultRangeBatchEnabled)
	if err != nil {
		return nil, err
	}
	rangeMaxRetries, err := setting.IntOptional(settings, rangeQueriesMaxRetriesSetting, defaultRangeMaxRetries)
	if err != nil {
		return nil, err
	}

	config := &Config{
		Secrets: &Secrets{
			SecretKey: setting.FirstDefined(settings, secretAccessKeySetting, secretKeySetting),
		},
		Region:                   settings[regionSetting],
		Endpoint:                 settings[endpointSetting],
		EndpointSource:           settings[endpointSourceSetting],
		EndpointPort:             port,
		Bucket:                   bucket,
		RootPath:                 rootPath,
		AccessKey:                setting.FirstDefined(settings, accessKeyIDSetting, accessKeySetting),
		SessionToken:             settings[sessionTokenSetting],
		RoleARN:                  settings[roleARNSetting],
		SessionName:              settings[sessionNameSetting],
		CACertFile:               settings[caCertFileSetting],
		UseYCSessionToken:        settings[useYcSessionTokenSetting],
		ForcePathStyle:           forcePathStyle,
		RequestAdditionalHeaders: settings[requestAdditionalHeadersSetting],
		UseListObjectsV1:         useListObjectsV1,
		MaxRetries:               maxRetries,
		LogLevel:                 settings[logLevelSetting],
		Uploader: &UploaderConfig{
			UploadConcurrency:            uploadConcurrency,
			MaxPartSize:                  maxPartSize,
			StorageClass:                 storageClass,
			ServerSideEncryption:         settings[sseSetting],
			ServerSideEncryptionCustomer: settings[sseCSetting],
			ServerSideEncryptionKMSID:    settings[sseKmsIDSetting],
		},
		RangeBatchEnabled: rangeBatchEnabled,
		RangeMaxRetries:   rangeMaxRetries,
	}

	st, err := NewStorage(config, rootWraps...)
	if err != nil {
		return nil, fmt.Errorf("create S3 storage: %w", err)
	}
	return st, nil
}
