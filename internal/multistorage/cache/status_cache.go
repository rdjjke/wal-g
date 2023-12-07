package cache

import (
	"fmt"
	"time"

	"github.com/wal-g/tracelog"
)

type Cache interface {
	// Read the cached state for storages with specified names and split them by relevance according to the cache TTL.
	Read(storageNames ...string) (relevant, outdated AliveMap, err error)

	// ApplyExplicitCheckResult with specifying which checked storages were alive, and return the new cached state for
	// all storages with specified names.
	ApplyExplicitCheckResult(checkResult AliveMap, storageNames ...string) (AliveMap, error)

	// ApplyOperationResult to the cache for a specific storage, indicating whether the storage was alive, and the
	// weight of the performed operation.
	ApplyOperationResult(storage string, alive bool, weight int)

	// Flush changes made in memory to the shared cache file.
	Flush()
}

var _ Cache = &cache{}

type cache struct {
	// usedKeys matches all storage names that can be requested from this cache with corresponding keys.
	usedKeys map[string]Key
	ttl      time.Duration

	// shMem keeps the intra-process cache that's shared among different threads and subsequent storages requests.
	shMem *SharedMemory

	// shFile is the path to the file that keeps the inter-process cache that's shared between different command executions.
	shFile             *SharedFile
	shFileUsed         bool
	shFileFlushTimeout time.Duration
}

func New(
	usedKeys map[string]Key,
	ttl time.Duration,
	sharedMem *SharedMemory,
	sharedFile *SharedFile,
) Cache {
	return &cache{
		usedKeys:           usedKeys,
		ttl:                ttl,
		shMem:              sharedMem,
		shFile:             sharedFile,
		shFileUsed:         sharedFile != nil,
		shFileFlushTimeout: 5 * time.Minute,
	}
}

func (c *cache) Read(storageNames ...string) (relevant, outdated AliveMap, err error) {
	c.shMem.Lock()
	defer c.shMem.Unlock()

	storageKeys, err := c.correspondingKeys(storageNames...)
	if err != nil {
		return nil, nil, err
	}

	allMemRelevant := c.shMem.Statuses.isRelevant(c.ttl, storageKeys...)
	if allMemRelevant {
		return c.shMem.Statuses.aliveMap(), nil, nil
	}

	fileStatuses, err := c.shFile.read()
	if err != nil {
		tracelog.WarningLogger.Printf("Failed to read storage status cache file %q: %v", c.shFile.Path, err)
	}
	memAndFileMerged := mergeByRelevance(c.shMem.Statuses, fileStatuses)

	c.shMem.Statuses = memAndFileMerged

	relevantStatuses, outdatedStatuses := memAndFileMerged.splitByRelevance(c.ttl, storageKeys)
	return relevantStatuses.aliveMap(), outdatedStatuses.aliveMap(), nil
}

func (c *cache) ApplyExplicitCheckResult(checkResult AliveMap, storageNames ...string) (AliveMap, error) {
	c.shMem.Lock()
	defer c.shMem.Unlock()

	checkResultByKeys := make(map[Key]bool, len(checkResult))
	for _, key := range c.usedKeys {
		if res, ok := checkResult[key.Name]; ok {
			checkResultByKeys[key] = res
		}
	}

	c.shMem.Statuses = c.shMem.Statuses.applyExplicitCheckResult(checkResultByKeys)

	storageKeys, err := c.correspondingKeys(storageNames...)
	if err != nil {
		return nil, err
	}
	aliveMap := c.shMem.Statuses.filter(storageKeys).aliveMap()

	if !c.shFileUsed {
		return aliveMap, nil
	}
	shFileRelevant := time.Since(c.shFile.Updated) < c.shFileFlushTimeout
	if shFileRelevant {
		return aliveMap, nil
	}
	err = c.shFile.write(c.shMem.Statuses)
	if err != nil {
		tracelog.WarningLogger.Printf("Failed to apply check result to storage status cache file %q: %v", c.shFile.Path, err)
	}
	return aliveMap, nil
}

func (c *cache) correspondingKeys(names ...string) ([]Key, error) {
	keys := make([]Key, len(names))
	for _, name := range names {
		key, ok := c.usedKeys[name]
		if !ok {
			return nil, fmt.Errorf("unknown storage %q", name)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func (c *cache) ApplyOperationResult(storage string, alive bool, weight int) {
	c.shMem.Lock()
	defer c.shMem.Unlock()

	var key Key
	for _, k := range c.usedKeys {
		if k.Name == storage {
			key = k
			break
		}
	}

	c.shMem.Statuses[key] = c.shMem.Statuses[key].applyOperationResult(alive, weight, time.Now())

	if !c.shFileUsed {
		return
	}
	shFileRelevant := time.Since(c.shFile.Updated) < c.shFileFlushTimeout
	if shFileRelevant {
		return
	}
	err := c.shFile.write(c.shMem.Statuses)
	if err != nil {
		tracelog.WarningLogger.Printf("Failed to apply operation result to storage status cache file %q: %v", c.shFile.Path, err)
	}
}

func (c *cache) Flush() {
	if !c.shFileUsed {
		return
	}

	c.shMem.Lock()
	defer c.shMem.Unlock()

	fileStatuses, err := c.shFile.read()
	if err != nil {
		tracelog.WarningLogger.Printf("Failed to read storage status cache file to merge it with memory %q: %v", c.shFile.Path, err)
	}
	memAndFileMerged := mergeByRelevance(c.shMem.Statuses, fileStatuses)
	err = c.shFile.write(memAndFileMerged)
	if err != nil {
		tracelog.WarningLogger.Printf("Failed to flush storage status cache file: %v", err)
	}
}
