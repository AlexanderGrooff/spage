package common

import (
	"reflect"
	"sync"
)

// SyncMapToMap converts a sync.Map to a regular map[string]interface{}.
// Note: This provides a snapshot. Concurrent modifications to the original
// sync.Map after this call won't be reflected in the returned map.
func SyncMapToMap(m *sync.Map) map[string]interface{} {
	regularMap := make(map[string]interface{})
	if m == nil {
		return regularMap
	}
	m.Range(func(key, value interface{}) bool {
		if keyStr, ok := key.(string); ok {
			regularMap[keyStr] = value
		}
		// Decide how to handle non-string keys if they are possible
		return true // continue iteration
	})
	return regularMap
}

// InterfaceToSlice attempts to convert an interface{} to a []interface{}.
// It handles cases where the underlying type is already []interface{}
// or a slice of a specific type (e.g., []string, []int).
func InterfaceToSlice(value interface{}) ([]interface{}, bool) {
	if value == nil {
		return nil, false
	}

	val := reflect.ValueOf(value)
	if val.Kind() != reflect.Slice {
		return nil, false
	}

	length := val.Len()
	slice := make([]interface{}, length)
	for i := 0; i < length; i++ {
		slice[i] = val.Index(i).Interface()
	}
	return slice, true
}

// CopyMap creates a shallow copy of a map[string]interface{}.
func CopyMap(original map[string]interface{}) map[string]interface{} {
	if original == nil {
		return nil
	}
	newMap := make(map[string]interface{}, len(original))
	for key, value := range original {
		newMap[key] = value
	}
	return newMap
}
