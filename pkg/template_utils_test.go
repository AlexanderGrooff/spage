package pkg

import (
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestClosure creates a basic closure for testing purposes.
func setupTestClosure() *Closure {
	facts := new(sync.Map)
	facts.Store("friend", "World")
	facts.Store("nested_var", "found")
	facts.Store("should_run", true)

	host := &Host{
		Name: "testhost",
		Vars: map[string]interface{}{
			"host_var": "host_value",
		},
	}
	hostContext := &HostContext{
		Host:  host,
		Facts: facts,
	}
	return &Closure{
		HostContext: hostContext,
		ExtraFacts:  map[string]interface{}{"extra_var": "extra_value"},
	}
}

// TestProcessRecursive provides comprehensive tests for the ProcessRecursive function.
func TestProcessRecursive(t *testing.T) {
	closure := setupTestClosure()

	// Define test structs
	type SimpleStruct struct {
		Greeting string
		Number   int
	}

	type NestedStruct struct {
		Name      string
		Details   SimpleStruct
		DetailsP  *SimpleStruct
		Interface interface{}
	}

	testCases := []struct {
		name     string
		input    interface{}
		expected interface{}
		hasError bool
	}{
		{
			name:     "Simple String",
			input:    "Hello, {{ friend }}!",
			expected: "Hello, World!",
		},
		{
			name:     "String with multiple variables",
			input:    "Host: {{ host }}, Var: {{ host_var }}, Extra: {{ extra_var }}",
			expected: "Host: testhost, Var: host_value, Extra: extra_value",
		},
		{
			name:     "String with no template",
			input:    "Just a plain string.",
			expected: "Just a plain string.",
		},
		{
			name:     "Invalid Template",
			input:    "Hello, {{ friend | non_existent_filter }}",
			hasError: true,
		},
		{
			name: "Simple Struct",
			input: SimpleStruct{
				Greeting: "Hello, {{ friend }}",
				Number:   123,
			},
			expected: SimpleStruct{
				Greeting: "Hello, World",
				Number:   123,
			},
		},
		{
			name: "Pointer to Struct",
			input: &SimpleStruct{
				Greeting: "Pointer says hi to {{ friend }}",
				Number:   456,
			},
			expected: &SimpleStruct{
				Greeting: "Pointer says hi to World",
				Number:   456,
			},
		},
		{
			name:     "Nil Pointer",
			input:    (*SimpleStruct)(nil),
			expected: (*SimpleStruct)(nil),
		},
		{
			name: "Nested Struct",
			input: NestedStruct{
				Name: "Test {{ friend }}",
				Details: SimpleStruct{
					Greeting: "Nested hello",
					Number:   789,
				},
				DetailsP: &SimpleStruct{
					Greeting: "Nested pointer {{ nested_var }}",
				},
				Interface: "Interface value: {{ host_var }}",
			},
			expected: NestedStruct{
				Name: "Test World",
				Details: SimpleStruct{
					Greeting: "Nested hello",
					Number:   789,
				},
				DetailsP: &SimpleStruct{
					Greeting: "Nested pointer found",
				},
				Interface: "Interface value: host_value",
			},
		},
		{
			name:     "Slice of Strings",
			input:    []string{"A", "B: {{ friend }}", "C"},
			expected: []string{"A", "B: World", "C"},
		},
		{
			name: "Slice of Structs",
			input: []SimpleStruct{
				{Greeting: "First: {{ friend }}"},
				{Greeting: "Second: {{ host_var }}"},
			},
			expected: []SimpleStruct{
				{Greeting: "First: World"},
				{Greeting: "Second: host_value"},
			},
		},
		{
			name:     "Map of Strings",
			input:    map[string]string{"key1": "Value for {{ friend }}", "key2": "Static"},
			expected: map[string]string{"key1": "Value for World", "key2": "Static"},
		},
		{
			name: "Map of interface{}",
			input: map[string]interface{}{
				"greeting":  "Hi, {{ friend }}",
				"number":    123,
				"is_true":   "{{ should_run }}",
				"nestedMap": map[string]string{"deep": "Deep value {{ nested_var }}"},
				"nestedSlice": []interface{}{
					"Slice value: {{ host_var }}",
					999,
				},
			},
			expected: map[string]interface{}{
				"greeting":  "Hi, World",
				"number":    123,
				"is_true":   "true",
				"nestedMap": map[string]string{"deep": "Deep value found"},
				"nestedSlice": []interface{}{
					"Slice value: host_value",
					999,
				},
			},
		},
		{
			name: "Complex Nested Structure",
			input: &NestedStruct{
				Name: "Complex Test",
				DetailsP: &SimpleStruct{
					Greeting: "Complex {{ friend }}",
				},
				Interface: []map[string]interface{}{
					{
						"key1": "Value {{ host_var }}",
						"key2": []string{"{{ nested_var }}"},
					},
				},
			},
			expected: &NestedStruct{
				Name: "Complex Test",
				DetailsP: &SimpleStruct{
					Greeting: "Complex World",
				},
				Interface: []map[string]interface{}{
					{
						"key1": "Value host_value",
						"key2": []string{"found"},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			originalVal := reflect.ValueOf(tc.input)

			processedVal, err := ProcessRecursive(originalVal, closure)

			if tc.hasError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.True(t, processedVal.IsValid(), "Processed value should be valid")

				processedInterface := processedVal.Interface()

				// Use testify's require for deep equality checks
				assert.Equal(t, tc.expected, processedInterface)
			}
		})
	}
}
