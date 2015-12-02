package tsm1

import (
	"fmt"
	"math"
	"sort"
	"sync"
)

var ErrCacheMemoryExceeded = fmt.Errorf("cache maximum memory size exceeded")
var ErrCacheInvalidCheckpoint = fmt.Errorf("invalid checkpoint")

// entry is a set of values and some metadata.
type entry struct {
	values   Values // All stored values.
	needSort bool   // true if the values are out of order and require deduping.
}

// newEntry returns a new instance of entry.
func newEntry() *entry {
	return &entry{}
}

// add adds the given values to the entry.
func (e *entry) add(values []Value) {
	// if there are existing values make sure they're all less than the first of
	// the new values being added
	l := len(e.values)
	if l != 0 {
		lastValTime := e.values[0].UnixNano()
		if lastValTime >= values[0].UnixNano() {
			e.needSort = true
		}
	}
	e.values = append(e.values, values...)
	e.size += uint64(Values(values).Size())

	// if there's only one value, we know it's sorted
	if len(values) == 1 {
		return
	}

	// make sure the new values were in sorted order
	min := int64(math.MinInt64)
	for _, v := range values {
		if min >= v.UnixNano() {
			e.needSort = true
			break
		}
	}
}

// Cache maintains an in-memory store of Values for a set of keys.
type Cache struct {
	mu      sync.RWMutex
	store   map[string]*entry
	size    uint64
	maxSize uint64

	// flushingCaches are the cache objects that are currently being written to tsm files
	// they're kept in memory while flushing so they can be queried along with the cache.
	// they are read only and should never be modified
	flushingCaches     []*Cache
	flushingCachesSize uint64
}

// NewCache returns an instance of a cache which will use a maximum of maxSize bytes of memory.
func NewCache(maxSize uint64) *Cache {
	return &Cache{
		maxSize: maxSize,
		store:   make(map[string]*entry),
	}
}

// Write writes the set of values for the key to the cache. This function is goroutine-safe.
// It returns the size of the cache after the write or an error if the cache has exceeded
// its max size.
func (c *Cache) Write(key string, values []Value) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Enough room in the cache?
	newSize := c.size + uint64(Values(values).Size())
	if newSize+c.flushingCachesSize > c.maxSize {
		return newSize, ErrCacheMemoryExceeded
	}

	c.write(key, values)
	c.size = newSize

	return newSize, nil
}

// WriteMulti writes the map of keys and associated values to the cache. This function is goroutine-safe.
// It returns the size of the cache after the write or an error if the cache has exceeded its max size.
func (c *Cache) WriteMulti(values map[string][]Value) (newSize, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	totalSz := 0
	for _, v := range values {
		totalSz += Values(v).Size()
	}

	// Enough room in the cache?
	newSize := c.size + uint64(totalSz)
	if newSize+c.flushingCachesSize > c.maxSize {
		return newSize, ErrCacheMemoryExceeded
	}

	for k, v := range values {
		c.write(k, v)
	}
	c.size = newSize

	return nil
}

// Snapshot will take a snapshot of the current cache, add it to the slice of caches that
// are being flushed, and reset the current cache with new values
func (c *Cache) Snapshot() *Cache {
	c.mu.Lock()
	defer c.mu.Unlock()

	snapshot := NewCache(c.maxSize)
	snapshot.store = c.store
	snapshot.size = c.size

	c.store = make(map[string]*entry)
	c.size = 0

	c.flushingCaches = append(c.flushingCachesSize, snap)
	c.flushingCachesSize += snapshot.size

	return snapshot
}

// ClearSnapshot will remove the snapshot cache from the list of flushing caches and
// adjust the size
func (c *Cache) ClearSnapshot(snapshot *Cache) {
	c.mu.Lock()
	defer c.mu.Unlock()

	caches := make([]*Cache, 0)
	cleared := false
	for _, cache := range c.flushingCaches {
		if cache != snapshot {
			caches = append(caches, cache)
		} else {
			cleared = true
		}
	}

	c.flushingCaches = caches

	// update the size if the snapshot was cleared from the flushing caches
	if cleared {
		c.size -= snapshot.size
	}
}

// Size returns the number of point-calcuated bytes the cache currently uses.
func (c *Cache) Size() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.size + c.flushingCachesSize
}

// MaxSize returns the maximum number of bytes the cache may consume.
func (c *Cache) MaxSize() uint64 {
	return c.maxSize
}

// Keys returns a sorted slice of all keys under management by the cache.
func (c *Cache) Keys() []string {
	var a []string
	for k, _ := range c.store {
		a = append(a, k)
	}
	sort.Strings(a)
	return a
}

// Values returns a copy of all values, deduped and sorted, for the given key.
func (c *Cache) Values(key string) Values {
	values, needSort := func() (Values, bool) {
		c.mu.RLock()
		defer c.mu.RUnlock()
		e := c.store[key]
		if e == nil {
			return nil, false
		}

		if e.needSort {
			return nil, true
		}

		return e.values[0:len(values)], false
	}()

	// the values in the entry require a sort, do so with a write lock so
	// we can sort once and set everything in order
	if needSort {
		values = func() Values {
			c.mu.Lock()
			defer c.mu.Unlock()

			e := c.store[key]
			if e == nil {
				return nil
			}
			e.values = e.values.Deduplicate()
			e.needSort = false

			return e.values[0:len(e.values)]
		}
	}

	return values
}

// write writes the set of values for the key to the cache. This function assumes
// the lock has been taken and does not enforce the cache size limits.
func (c *Cache) write(key string, values []Value) {
	e, ok := c.store[key]
	if !ok {
		e = newEntry()
		c.store[key] = e
	}
	e.add(values)
}
