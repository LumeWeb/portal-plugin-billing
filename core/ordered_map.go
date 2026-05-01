package core

import "sync"

// OrderedMap is a minimal generic map that preserves insertion order.
// Iteration via Range or Keys always yields entries in the order they were first inserted.
// Re-setting an existing key updates the value but does not change its position.
type OrderedMap[K comparable, V any] struct {
	mu   sync.RWMutex
	keys []K
	data map[K]V
}

// NewOrderedMap creates an empty OrderedMap.
func NewOrderedMap[K comparable, V any]() *OrderedMap[K, V] {
	return &OrderedMap[K, V]{
		keys: make([]K, 0),
		data: make(map[K]V),
	}
}

// Set stores the value for key. If the key already exists, the value is
// updated but the original insertion position is preserved.
func (m *OrderedMap[K, V]) Set(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.data[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.data[key] = value
}

// Get returns the value for key and whether it was present.
func (m *OrderedMap[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	v, ok := m.data[key]
	return v, ok
}

// Delete removes the entry for key and compacts the internal key ordering.
func (m *OrderedMap[K, V]) Delete(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.data[key]; !exists {
		return
	}
	delete(m.data, key)

	for i, k := range m.keys {
		if k == key {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			return
		}
	}
}

// Len returns the number of entries in the map.
func (m *OrderedMap[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.data)
}

// Range calls fn for each entry in insertion order. Iteration stops early
// if fn returns false.
func (m *OrderedMap[K, V]) Range(fn func(key K, value V) bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, k := range m.keys {
		if !fn(k, m.data[k]) {
			return
		}
	}
}

// Keys returns all keys in insertion order.
func (m *OrderedMap[K, V]) Keys() []K {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys := make([]K, len(m.keys))
	copy(keys, m.keys)
	return keys
}
