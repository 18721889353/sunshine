## cache

memory and redis cache libraries.

## Features

- Redis and in-memory cache implementations
- Distributed locking with redsync
- Bloom filter support for reducing cache misses
- Automatic serialization with multiple encoding options
- Comprehensive logging and error handling

## Example of use

```go

// Choose to create a memory or redis cache depending on CType
cache := cache.NewUserExampleCache(&model.CacheType{
  CType: "redis",
  Rdb:   c.RedisClient,
})

// -----------------------------------------------------------------------------------------

type userExampleDao struct {
	db    *gorm.DB
	cache cache.UserExampleCache
}

// NewUserExampleDao creating the dao interface
func NewUserExampleDao(db *gorm.DB, cache cache.UserExampleCache) UserExampleDao {
	return &userExampleDao{db: db, cache: cache}
}

// GetByID get a record based on id
func (d *userExampleDao) GetByID(ctx context.Context, id uint64) (*model.UserExample, error) {
	record, err := d.cache.Get(ctx, id)
	if err == nil {
		return record, nil
	}

	if errors.Is(err, model.ErrCacheNotFound) {
		// get from mysql
		table := &model.UserExample{}
		err = d.db.WithContext(ctx).Where("id = ?", id).First(table).Error
		if err != nil {
			// if data is empty, set not found cache to prevent cache penetration(preventing Cache Penetration)
			if errors.Is(err, model.ErrRecordNotFound) {
				err = d.cache.SetCacheWithNotFound(ctx, id)
				if err != nil {
					return nil, err
				}
				return nil, model.ErrRecordNotFound
			}
			return nil, err
		}

		// set cache
		err = d.cache.Set(ctx, id, table, 10*time.Minute)
		if err != nil {
			return nil, fmt.Errorf("cache.Set error: %v, id=%d", err, id)
		}
		return table, nil
	} else if errors.Is(err, cacheBase.ErrPlaceholder) {
		return nil, model.ErrRecordNotFound
	}

	// fail fast, if cache error return, don't request to db
	return nil, err
}
```

## Bloom Filter Features

The Redis cache implementation includes a Bloom filter to reduce unnecessary Redis queries:

- **False Positive Rate**: Configured at 0.1% by default
- **Expected Elements**: 10,000,000 by default
- **Auto-initialization**: Automatically populated from existing Redis keys
- **Statistics Tracking**: Tracks hits, misses, false positives, and true negatives
- **Rebuilding**: Supports both synchronous and asynchronous rebuilding of the filter
- **Health Monitoring**: Provides health metrics to monitor filter effectiveness

### Bloom Filter Methods

- `InitBloomFilter`: Initialize the Bloom filter from existing Redis keys
- `AddToBloomFilter`: Add a key to the Bloom filter
- `BloomFilter`: Test if a key might be in the cache
- `GetBloomFilterStats`: Get statistics about the Bloom filter usage
- `CheckBloomFilterHealth`: Get health metrics of the Bloom filter
- `RebuildBloomFilter`: Synchronously rebuild the Bloom filter
- `RebuildBloomFilterAsync`: Asynchronously rebuild the Bloom filter

The Bloom filter helps reduce cache penetration and unnecessary database queries by quickly determining if a key definitely does not exist in the cache.