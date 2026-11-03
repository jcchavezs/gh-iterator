// This example clones a repository 100 times with RunForRepository. If the CACHE_KEY env var is
// passed with a non empty value it clones the repository once to a cache (CloneCacheKey) and reuses
// copies of it. If the same value is passed again to a new execution it will reuse the cache.
package main
