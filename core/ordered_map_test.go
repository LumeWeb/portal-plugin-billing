package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewOrderedMap(t *testing.T) {
	m := NewOrderedMap[string, int]()
	assert.NotNil(t, m)
	assert.Equal(t, 0, m.Len())
}

func TestOrderedMap_SetAndGet(t *testing.T) {
	m := NewOrderedMap[string, int]()

	v, ok := m.Get("a")
	assert.False(t, ok)
	assert.Zero(t, v)

	m.Set("a", 1)
	v, ok = m.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 1, v)
}

func TestOrderedMap_SetUpdatesValue(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("a", 1)
	m.Set("a", 2)

	v, ok := m.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 2, v)
	assert.Equal(t, 1, m.Len())
}

func TestOrderedMap_PreservesInsertionOrder(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("c", 3)
	m.Set("a", 1)
	m.Set("b", 2)

	var keys []string
	m.Range(func(k string, v int) bool {
		keys = append(keys, k)
		return true
	})

	assert.Equal(t, []string{"c", "a", "b"}, keys)
}

func TestOrderedMap_RangeStopEarly(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	var collected []string
	m.Range(func(k string, v int) bool {
		collected = append(collected, k)
		return k != "b"
	})

	assert.Equal(t, []string{"a", "b"}, collected)
}

func TestOrderedMap_Keys(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("c", 3)
	m.Set("a", 1)
	m.Set("b", 2)

	assert.Equal(t, []string{"c", "a", "b"}, m.Keys())
}

func TestOrderedMap_Delete(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	m.Delete("b")

	_, ok := m.Get("b")
	assert.False(t, ok)
	assert.Equal(t, 2, m.Len())
}

func TestOrderedMap_DeletePreservesOrder(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	m.Delete("b")

	var keys []string
	m.Range(func(k string, v int) bool {
		keys = append(keys, k)
		return true
	})

	assert.Equal(t, []string{"a", "c"}, keys)
	assert.Equal(t, []string{"a", "c"}, m.Keys())
}

func TestOrderedMap_DeleteNonExistent(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("a", 1)

	m.Delete("z")

	assert.Equal(t, 1, m.Len())
}

func TestOrderedMap_SetAfterDelete(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)

	m.Delete("a")
	// After delete, "a" is removed from keys slice entirely
	// Re-setting "a" appends it at the end
	m.Set("a", 10)

	v, ok := m.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 10, v)
	assert.Equal(t, 2, m.Len())

	var keys []string
	m.Range(func(k string, v int) bool {
		keys = append(keys, k)
		return true
	})

	assert.Equal(t, []string{"b", "a"}, keys)
}

func TestOrderedMap_UpdateDoesNotChangePosition(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	m.Set("a", 100)

	var keys []string
	m.Range(func(k string, v int) bool {
		keys = append(keys, k)
		return true
	})

	assert.Equal(t, []string{"a", "b", "c"}, keys)

	v, ok := m.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 100, v)
}

func TestOrderedMap_EmptyRange(t *testing.T) {
	m := NewOrderedMap[string, int]()

	called := false
	m.Range(func(k string, v int) bool {
		called = true
		return true
	})

	assert.False(t, called)
}

func TestOrderedMap_EmptyKeys(t *testing.T) {
	m := NewOrderedMap[string, int]()
	assert.Empty(t, m.Keys())
}

func TestOrderedMap_DeletedKeysSkippedInKeys(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	m.Delete("a")
	m.Delete("c")

	assert.Equal(t, []string{"b"}, m.Keys())
}

func TestOrderedMap_Clone(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	clone := m.Clone()

	assert.Equal(t, m.Len(), clone.Len())
	assert.Equal(t, m.Keys(), clone.Keys())

	clone.Set("d", 4)
	assert.Equal(t, 3, m.Len())
	assert.Equal(t, 4, clone.Len())

	m.Set("a", 100)
	v, ok := clone.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 1, v)
}

func TestOrderedMap_CloneEmpty(t *testing.T) {
	m := NewOrderedMap[string, int]()
	clone := m.Clone()

	assert.Equal(t, 0, clone.Len())
	assert.Empty(t, clone.Keys())
}
