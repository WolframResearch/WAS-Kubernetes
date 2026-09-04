package handlers

import (
	"sync"
	"time"
)

// CredentialCache stores the result of detectAWSAvailability and
// detectAzureAvailability for a configurable TTL, preventing repeated
// subprocess forks on every HTTP request (including 10-second background polls).
//
// The cache is nil by default (disabled in tests). EnableCredentialCache
// activates it in production via NewServer.
type CredentialCache struct {
	mu    sync.RWMutex
	aws   map[string]*awsCacheEntry // key: region
	azure *azureCacheEntry
	ttl   time.Duration
}

type awsCacheEntry struct {
	avail CloudAvailability
	at    time.Time
}

type azureCacheEntry struct {
	avail CloudAvailability
	subs  []AzureSubscription
	at    time.Time
}

func newCredentialCache(ttl time.Duration) *CredentialCache {
	return &CredentialCache{
		aws: make(map[string]*awsCacheEntry),
		ttl: ttl,
	}
}

func (c *CredentialCache) getAWS(region string) (CloudAvailability, bool) {
	c.mu.RLock()
	entry := c.aws[region]
	c.mu.RUnlock()
	if entry != nil && time.Since(entry.at) < c.ttl {
		return entry.avail, true
	}
	return CloudAvailability{}, false
}

func (c *CredentialCache) setAWS(region string, avail CloudAvailability) {
	c.mu.Lock()
	c.aws[region] = &awsCacheEntry{avail: avail, at: time.Now()}
	c.mu.Unlock()
}

func (c *CredentialCache) getAzure() (CloudAvailability, []AzureSubscription, bool) {
	c.mu.RLock()
	entry := c.azure
	c.mu.RUnlock()
	if entry != nil && time.Since(entry.at) < c.ttl {
		return entry.avail, entry.subs, true
	}
	return CloudAvailability{}, nil, false
}

func (c *CredentialCache) setAzure(avail CloudAvailability, subs []AzureSubscription) {
	c.mu.Lock()
	c.azure = &azureCacheEntry{avail: avail, subs: subs, at: time.Now()}
	c.mu.Unlock()
}

// serverCredCache is the per-process credential cache.
// nil = disabled (the default; tests never call EnableCredentialCache).
var serverCredCache *CredentialCache

// EnableCredentialCache activates TTL-based caching of cloud credential detection.
// Safe to call once from NewServer during startup; not safe for concurrent calls.
func EnableCredentialCache(ttl time.Duration) {
	serverCredCache = newCredentialCache(ttl)
}
