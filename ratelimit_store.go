package ginx

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimitStore defines the interface for storing and managing rate limiters.
// It provides methods to store, retrieve, and manage rate.Limiter instances by key.
//
// Locking contract for implementations:
// The rate limiter may call Store.Set while holding an internal creation mutex (createMu).
// Implementations MUST NOT call back into the rate limiter or acquire locks that could
// be held by callers of Get/Set. The safe pattern is: each store method acquires only
// its own internal lock, with no external dependencies. The built-in MemoryLimiterStore
// follows this pattern (createMu → store.mu, always in this order, never reversed).
type RateLimitStore interface {
	// Get returns the limiter for the given key
	Get(key string) (*rate.Limiter, bool)
	// Set stores the limiter for the given key
	Set(key string, limiter *rate.Limiter)
	// Delete removes the limiter for the given key
	Delete(key string)
	// Clear removes all expired limiters
	Clear()
	// Close cleans up resources
	Close() error
}

// WindowCounterStore defines the interface for storing time-window based counters.
// Used for minute/hour/day rate limiting with fixed window algorithm.
type WindowCounterStore interface {
	// Increment increments the counter for the given key and window, returns new count
	Increment(key string, window time.Time) (int64, error)
	// IncrementWithinLimit atomically increments the counter when under limit.
	// It returns the resulting count, whether the increment happened, and any errors.
	IncrementWithinLimit(key string, window time.Time, limit int64) (count int64, allowed bool, err error)
	// Get returns the current count for the given key and window
	Get(key string, window time.Time) (int64, error)
	// Clear removes expired counters
	Clear()
	// Close cleans up resources
	Close() error
}

// MemoryLimiterStore provides a thread-safe, in-memory implementation of RateLimitStore.
// It automatically cleans up expired limiters to prevent memory leaks and is registered
// globally for automatic resource management.
type MemoryLimiterStore struct {
	mu         sync.RWMutex
	limiters   map[string]*rate.Limiter
	lastAccess map[string]time.Time
	maxIdle    time.Duration

	// Cleanup goroutine control
	ticker    *time.Ticker
	done      chan struct{}
	closeOnce sync.Once
}

// MemoryWindowCounterStore provides a thread-safe, in-memory implementation of WindowCounterStore.
// It uses a fixed window algorithm for time-based rate limiting (minute/hour/day).
type MemoryWindowCounterStore struct {
	mu         sync.RWMutex
	counters   map[string]int64     // key:window -> count
	lastAccess map[string]time.Time // key:window -> last access time
	maxIdle    time.Duration

	// Cleanup goroutine control
	ticker    *time.Ticker
	done      chan struct{}
	closeOnce sync.Once
}

var (
	// Global registry of all active stores for automatic cleanup
	activeStores      = make(map[RateLimitStore]struct{})
	activeStoresMutex sync.RWMutex

	// Global registry of window counter stores
	activeWindowStores      = make(map[WindowCounterStore]struct{})
	activeWindowStoresMutex sync.RWMutex

	// Global default store shared by all rate limiters.
	// Protected by defaultStoreMu during initialization and cleanup.
	defaultStore     RateLimitStore
	defaultStoreOnce sync.Once
	defaultStoreMu   sync.Mutex

	// Global default window counter store.
	// Protected by defaultWindowStoreMu during initialization and cleanup.
	defaultWindowStore     WindowCounterStore
	defaultWindowStoreOnce sync.Once
	defaultWindowStoreMu   sync.Mutex
)

func cleanupTickerInterval(maxIdle time.Duration) time.Duration {
	interval := maxIdle / 2
	if interval <= 0 {
		return time.Nanosecond
	}
	return interval
}

// NewMemoryLimiterStore creates a thread-safe in-memory store with automatic cleanup.
//
// Parameters:
//   - maxIdle: Duration to keep unused limiters (defaults to 5 minutes if <= 0)
//
// Resource Management:
// The store is automatically registered globally and cleaned up by CleanupRateLimiters().
// Manual Close() is optional unless immediate cleanup is needed.
func NewMemoryLimiterStore(maxIdle time.Duration) RateLimitStore {
	if maxIdle <= 0 {
		maxIdle = 5 * time.Minute
	}

	store := &MemoryLimiterStore{
		limiters:   make(map[string]*rate.Limiter),
		lastAccess: make(map[string]time.Time),
		maxIdle:    maxIdle,
		done:       make(chan struct{}),
	}

	// Start cleanup goroutine
	store.ticker = time.NewTicker(cleanupTickerInterval(maxIdle))
	go store.cleanup()

	// Register for automatic cleanup
	activeStoresMutex.Lock()
	activeStores[store] = struct{}{}
	activeStoresMutex.Unlock()

	return store
}

// Get retrieves a rate limiter for the given key and updates its last access time.
// To reduce write lock contention under high concurrency, lastAccess is only updated
// when more than 1 second has elapsed since the last update. This is safe because
// the maxIdle duration (typically minutes) is much larger than the 1-second threshold.
func (s *MemoryLimiterStore) Get(key string) (*rate.Limiter, bool) {
	s.mu.RLock()
	limiter, exists := s.limiters[key]
	var lastAccess time.Time
	if exists {
		lastAccess = s.lastAccess[key]
	}
	s.mu.RUnlock()

	// Only acquire write lock to update lastAccess if stale (>1s since last update)
	if exists && time.Since(lastAccess) > time.Second {
		s.mu.Lock()
		s.lastAccess[key] = time.Now()
		s.mu.Unlock()
	}

	return limiter, exists
}

// Set stores a rate limiter for the given key and records the access time.
func (s *MemoryLimiterStore) Set(key string, limiter *rate.Limiter) {
	s.mu.Lock()
	s.limiters[key] = limiter
	s.lastAccess[key] = time.Now()
	s.mu.Unlock()
}

// Delete removes a rate limiter and its access time record.
func (s *MemoryLimiterStore) Delete(key string) {
	s.mu.Lock()
	delete(s.limiters, key)
	delete(s.lastAccess, key)
	s.mu.Unlock()
}

// Clear removes all stored rate limiters and access time records.
func (s *MemoryLimiterStore) Clear() {
	s.mu.Lock()
	s.limiters = make(map[string]*rate.Limiter)
	s.lastAccess = make(map[string]time.Time)
	s.mu.Unlock()
}

// Close stops the cleanup goroutine and releases resources.
func (s *MemoryLimiterStore) Close() error {
	s.closeOnce.Do(func() {
		s.ticker.Stop()
		close(s.done)
		s.Clear()
		// Unregister from global cleanup
		activeStoresMutex.Lock()
		delete(activeStores, s)
		activeStoresMutex.Unlock()
	})
	return nil
}

// cleanup runs in a separate goroutine to remove expired rate limiters.
func (s *MemoryLimiterStore) cleanup() {
	for {
		select {
		case <-s.done:
			return
		case now := <-s.ticker.C:
			s.mu.Lock()
			for key, lastAccess := range s.lastAccess {
				if now.Sub(lastAccess) > s.maxIdle {
					delete(s.limiters, key)
					delete(s.lastAccess, key)
				}
			}
			s.mu.Unlock()
		}
	}
}

// NewMemoryWindowCounterStore creates a thread-safe in-memory window counter store with automatic cleanup.
//
// Parameters:
//   - maxIdle: Duration to keep unused counters (defaults to 25 hours if <= 0, sufficient for daily limits)
//
// Resource Management:
// The store is automatically registered globally and cleaned up by CleanupRateLimiters().
func NewMemoryWindowCounterStore(maxIdle time.Duration) WindowCounterStore {
	if maxIdle <= 0 {
		maxIdle = 25 * time.Hour // Default: keep counters for just over a day
	}

	store := &MemoryWindowCounterStore{
		counters:   make(map[string]int64),
		lastAccess: make(map[string]time.Time),
		maxIdle:    maxIdle,
		done:       make(chan struct{}),
	}

	// Start cleanup goroutine
	store.ticker = time.NewTicker(cleanupTickerInterval(maxIdle))
	go store.cleanupWindows()

	// Register for automatic cleanup
	activeWindowStoresMutex.Lock()
	activeWindowStores[store] = struct{}{}
	activeWindowStoresMutex.Unlock()

	return store
}

// Increment increments the counter for the given key and window, returns new count
func (s *MemoryWindowCounterStore) Increment(key string, window time.Time) (int64, error) {
	windowKey := formatWindowKey(key, window)
	s.mu.Lock()
	count := s.counters[windowKey] + 1
	s.counters[windowKey] = count
	s.lastAccess[windowKey] = time.Now()
	s.mu.Unlock()
	return count, nil
}

// Get returns the current count for the given key and window
func (s *MemoryWindowCounterStore) Get(key string, window time.Time) (int64, error) {
	windowKey := formatWindowKey(key, window)
	s.mu.RLock()
	count := s.counters[windowKey]
	s.mu.RUnlock()
	return count, nil
}

// IncrementWithinLimit atomically increments the current window counter when under limit.
func (s *MemoryWindowCounterStore) IncrementWithinLimit(key string, window time.Time, limit int64) (int64, bool, error) {
	windowKey := formatWindowKey(key, window)
	s.mu.Lock()
	defer s.mu.Unlock()

	count := s.counters[windowKey]
	if limit > 0 && count >= limit {
		return count, false, nil
	}

	count++
	s.counters[windowKey] = count
	s.lastAccess[windowKey] = time.Now()
	return count, true, nil
}

// Clear removes all stored counters and access time records.
func (s *MemoryWindowCounterStore) Clear() {
	s.mu.Lock()
	s.counters = make(map[string]int64)
	s.lastAccess = make(map[string]time.Time)
	s.mu.Unlock()
}

// Close stops the cleanup goroutine and releases resources.
func (s *MemoryWindowCounterStore) Close() error {
	s.closeOnce.Do(func() {
		s.ticker.Stop()
		close(s.done)
		s.Clear()
		// Unregister from global cleanup
		activeWindowStoresMutex.Lock()
		delete(activeWindowStores, s)
		activeWindowStoresMutex.Unlock()
	})
	return nil
}

// cleanupWindows runs in a separate goroutine to remove expired window counters.
func (s *MemoryWindowCounterStore) cleanupWindows() {
	for {
		select {
		case <-s.done:
			return
		case now := <-s.ticker.C:
			s.mu.Lock()
			for key, lastAccess := range s.lastAccess {
				if now.Sub(lastAccess) > s.maxIdle {
					delete(s.counters, key)
					delete(s.lastAccess, key)
				}
			}
			s.mu.Unlock()
		}
	}
}

// formatWindowKey creates a unique key for a time window
func formatWindowKey(key string, window time.Time) string {
	return fmt.Sprintf("%s:%d", key, window.Unix())
}
