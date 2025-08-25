package pkg

import (
	"fmt"
	"reflect"
	"strings"

	"encoding/json"

	"github.com/AlexanderGrooff/jinja-go"
)

type JinjaExpression struct {
	Expression string
}

func (e JinjaExpression) String() string {
	return e.Expression
}

type JinjaExpressionList []JinjaExpression

func (e JinjaExpression) Evaluate(closure *Closure) (any, error) {
	if closure == nil {
		return e.Expression, nil
	}
	res, err := EvaluateExpression(e.Expression, closure)
	if err != nil {
		return false, err
	}
	return res, nil
}

func (e JinjaExpression) IsTruthy(closure *Closure) bool {
	res, err := e.Evaluate(closure)
	if err != nil {
		return false
	}
	return jinja.IsTruthy(res)
}

func (e JinjaExpression) ToCode() string {
	return fmt.Sprintf("pkg.JinjaExpression{Expression: %q}", e.Expression)
}

func (e JinjaExpression) IsEmpty() bool {
	return e.Expression == ""
}

func (el JinjaExpressionList) ToCode() string {
	sb := strings.Builder{}
	sb.WriteString("pkg.JinjaExpressionList{")
	for i, item := range el {
		sb.WriteString(item.ToCode())
		if i < len(el)-1 {
			sb.WriteString(", ")
		}
	}
	sb.WriteString("}")
	return sb.String()
}

func (el JinjaExpressionList) IsTruthy(closure *Closure) bool {
	for _, item := range el {
		res, err := item.Evaluate(closure)
		if err != nil {
			return false
		}
		if !jinja.IsTruthy(res) {
			return false
		}
	}
	return true
}

func (el JinjaExpressionList) IsEmpty() bool {
	if len(el) == 0 {
		return true
	}
	for _, item := range el {
		if !item.IsEmpty() {
			return false
		}
	}
	return true
}

// UnmarshalYAML allows JinjaExpression to be parsed from YAML as a string or bool
func (e *JinjaExpression) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		*e = JinjaExpression{Expression: s}
		return nil
	}
	var b bool
	if err := unmarshal(&b); err == nil {
		*e = JinjaExpression{Expression: fmt.Sprintf("%v", b)}
		return nil
	}
	// Try to unmarshal as struct
	type alias JinjaExpression
	var tmp alias
	if err := unmarshal(&tmp); err == nil {
		*e = JinjaExpression(tmp)
		return nil
	}
	return fmt.Errorf("JinjaExpression must be string, bool, or object with Expression field")
}

// UnmarshalJSON allows JinjaExpression to be parsed from JSON as a string or bool
func (e *JinjaExpression) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*e = JinjaExpression{Expression: s}
		return nil
	}
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		*e = JinjaExpression{Expression: fmt.Sprintf("%v", b)}
		return nil
	}
	// Try to unmarshal as struct
	type alias JinjaExpression
	var tmp alias
	if err := json.Unmarshal(data, &tmp); err == nil {
		*e = JinjaExpression(tmp)
		return nil
	}
	return fmt.Errorf("JinjaExpression must be string, bool, or object with Expression field")
}

// UnmarshalYAML for JinjaExpressionList supports both a single value or a list
func (el *JinjaExpressionList) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var single JinjaExpression
	if err := unmarshal(&single); err == nil {
		*el = JinjaExpressionList{single}
		return nil
	}
	var list []JinjaExpression
	if err := unmarshal(&list); err == nil {
		*el = JinjaExpressionList(list)
		return nil
	}
	return fmt.Errorf("JinjaExpressionList must be a string, bool, or list of them")
}

// UnmarshalJSON for JinjaExpressionList supports both a single value or a list
func (el *JinjaExpressionList) UnmarshalJSON(data []byte) error {
	var single JinjaExpression
	if err := json.Unmarshal(data, &single); err == nil {
		*el = JinjaExpressionList{single}
		return nil
	}
	var list []JinjaExpression
	if err := json.Unmarshal(data, &list); err == nil {
		*el = JinjaExpressionList(list)
		return nil
	}
	return fmt.Errorf("JinjaExpressionList must be a string, bool, or list of them")
}

// ProcessRecursive is the core recursive function that creates a new reflect.Value
// based on originalVal, with string fields templated.
// It returns a new reflect.Value representing the copied and processed value, or an error.
func ProcessRecursive(originalVal reflect.Value, closure *Closure) (reflect.Value, error) {
	if !originalVal.IsValid() {
		// Return the invalid value as is; the caller's .Set might handle or error.
		return originalVal, nil
	}

	switch originalVal.Kind() {
	case reflect.String:
		origStr := originalVal.String()

		maxIterations := 10 // To prevent infinite loops
		for i := 0; i < maxIterations; i++ {
			templatedStr, err := TemplateString(origStr, closure)
			if err != nil {
				return reflect.Value{}, err // Propagate error
			}
			if templatedStr == origStr {
				return reflect.ValueOf(templatedStr), nil
			}
			origStr = templatedStr
		}
		return reflect.Value{}, fmt.Errorf("template expansion exceeded max iterations for: %s", originalVal.String())

	case reflect.Struct:
		originalStructType := originalVal.Type()
		// Create a new instance of the struct type (e.g., a zero-valued struct).
		newStructInstance := reflect.New(originalStructType).Elem()

		for i := 0; i < originalVal.NumField(); i++ {
			originalFieldVal := originalVal.Field(i)
			newStructField := newStructInstance.Field(i)
			fieldType := originalStructType.Field(i)

			if !newStructField.CanSet() {
				// If the field in the new struct instance is unexported or not settable,
				// it will retain its zero value from the reflect.New().Elem() instantiation.
				// We don't (and can't) copy the original unexported value here.
				continue
			}

			processedFieldVal, err := ProcessRecursive(originalFieldVal, closure)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("failed to process field %s: %w", fieldType.Name, err)
			}
			if processedFieldVal.IsValid() { // Ensure we don't try to set an invalid value
				newStructField.Set(processedFieldVal)
			}
		}
		return newStructInstance, nil

	case reflect.Ptr:
		if originalVal.IsNil() {
			return reflect.Zero(originalVal.Type()), nil // Return a new nil pointer of the same type
		}
		elemVal := originalVal.Elem()
		processedElemVal, err := ProcessRecursive(elemVal, closure)
		if err != nil {
			return reflect.Value{}, err
		}

		// Create a new pointer of the same type as originalVal and set its element.
		newPtrInstance := reflect.New(elemVal.Type())
		if processedElemVal.IsValid() {
			newPtrInstance.Elem().Set(processedElemVal)
		}
		return newPtrInstance, nil

	case reflect.Slice:
		if originalVal.IsNil() {
			return reflect.Zero(originalVal.Type()), nil // Return a new nil slice of the same type
		}
		// Create a new slice with the same type, length, and capacity.
		newSliceInstance := reflect.MakeSlice(originalVal.Type(), originalVal.Len(), originalVal.Cap())
		for j := 0; j < originalVal.Len(); j++ {
			originalElemVal := originalVal.Index(j)
			processedElemVal, err := ProcessRecursive(originalElemVal, closure)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("failed to process slice element %d: %w", j, err)
			}
			if processedElemVal.IsValid() {
				newSliceInstance.Index(j).Set(processedElemVal)
			}
		}
		return newSliceInstance, nil

	case reflect.Map:
		if originalVal.IsNil() {
			return reflect.Zero(originalVal.Type()), nil // Return a new nil map of the same type
		}
		// Create a new map of the same type.
		newMapInstance := reflect.MakeMap(originalVal.Type())
		iter := originalVal.MapRange()
		for iter.Next() {
			key := iter.Key() // Keys are not templated, used as is.
			originalMapElemVal := iter.Value()

			processedMapElemVal, err := ProcessRecursive(originalMapElemVal, closure)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("failed to process map value for key %v: %w", key.Interface(), err)
			}
			if processedMapElemVal.IsValid() {
				newMapInstance.SetMapIndex(key, processedMapElemVal)
			}
		}
		return newMapInstance, nil

	case reflect.Interface:
		if originalVal.IsNil() {
			return reflect.Zero(originalVal.Type()), nil
		}
		// Recurse on the concrete value stored within the interface.
		elemVal := originalVal.Elem()
		processedElemVal, err := ProcessRecursive(elemVal, closure)
		if err != nil {
			return reflect.Value{}, err
		}
		// The parent setter (e.g., for a map or slice) will handle re-wrapping the processed value
		// into an interface{} if needed.
		return processedElemVal, nil

	default:
		// For basic types (int, bool, float, etc.), return the original value.
		// The .Set() method on the parent structure/slice/map will handle copying the value.
		return originalVal, nil
	}
}

// TemplateModuleInputFields creates a *copy* of the input provider's underlying struct,
// walking all string fields in the copied struct (recursively) and templates them.
// The original input struct is NOT mutated.
// It accepts a ConcreteModuleInputProvider and returns a new ConcreteModuleInputProvider
// of the same underlying kind (value or pointer) as the input, or an error.
func TemplateModuleInputFields(originalProvider ConcreteModuleInputProvider, closure *Closure) (ConcreteModuleInputProvider, error) {
	if originalProvider == nil {
		return nil, nil // Maintain behavior for nil inputs
	}

	originalInputVal := reflect.ValueOf(originalProvider)

	var originalStructVal reflect.Value
	wasPointerOriginal := false

	if originalInputVal.Kind() == reflect.Ptr {
		if originalInputVal.IsNil() {
			// Provider is an interface holding a nil pointer.
			return nil, nil // Nothing to template.
		}
		originalStructVal = originalInputVal.Elem()
		wasPointerOriginal = true
	} else if originalInputVal.Kind() == reflect.Struct {
		originalStructVal = originalInputVal
	} else {
		return nil, fmt.Errorf("input provider (type %T, kind %s) is not a struct or a pointer to a struct", originalProvider, originalInputVal.Kind())
	}

	// Ensure originalStructVal is actually a struct before proceeding
	if originalStructVal.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input provider's underlying type (type %T, kind %s after dereference if any) is not a struct", originalProvider, originalStructVal.Kind())
	}

	// Process the struct value recursively to get a new templated struct value.
	templatedStructVal, err := ProcessRecursive(originalStructVal, closure)
	if err != nil {
		return nil, err
	}

	if !templatedStructVal.IsValid() {
		return nil, fmt.Errorf("internal error: processed struct value is invalid after templating type %T", originalProvider)
	}

	var newProvider ConcreteModuleInputProvider
	if wasPointerOriginal {
		// Original was a pointer. Create a new pointer to the templated struct value.
		newPtrInstance := reflect.New(templatedStructVal.Type())
		newPtrInstance.Elem().Set(templatedStructVal)

		var ok bool
		newProvider, ok = newPtrInstance.Interface().(ConcreteModuleInputProvider)
		if !ok {
			return nil, fmt.Errorf("failed to assert new templated pointer (type %T) to ConcreteModuleInputProvider from original type %T", newPtrInstance.Interface(), originalProvider)
		}
	} else {
		// Original was a struct value. The templatedStructVal is the new struct value.
		var ok bool
		newProvider, ok = templatedStructVal.Interface().(ConcreteModuleInputProvider)
		if !ok {
			return nil, fmt.Errorf("failed to assert new templated value (type %T) to ConcreteModuleInputProvider from original type %T", templatedStructVal.Interface(), originalProvider)
		}
	}
	return newProvider, nil
}

func GetVariableUsageFromModule(input ConcreteModuleInputProvider) ([]string, error) {
	if input == nil {
		return nil, nil
	}

	var allVars []string

	// Use reflection to walk through all fields of the input
	inputVal := reflect.ValueOf(input)

	// Handle pointer types
	if inputVal.Kind() == reflect.Ptr {
		if inputVal.IsNil() {
			return nil, nil
		}
		inputVal = inputVal.Elem()
	}

	// Ensure we have a struct
	if inputVal.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input provider (type %T, kind %s) is not a struct or a pointer to a struct", input, inputVal.Kind())
	}

	// Recursively extract variables from the struct
	vars, err := extractVariablesFromValue(inputVal)
	if err != nil {
		return nil, fmt.Errorf("failed to extract variables from input: %w", err)
	}
	allVars = append(allVars, vars...)

	// Deduplicate variables
	uniqueVars := make(map[string]struct{})
	for _, v := range allVars {
		uniqueVars[v] = struct{}{}
	}

	result := make([]string, 0, len(uniqueVars))
	for k := range uniqueVars {
		result = append(result, k)
	}

	return result, nil
}

// JinjaStringToStringList Evaluate string list "['abc', 'def']" into Golang []string{"abc", "def"}
func JinjaStringToStringList(jinjaStr string) ([]string, error) {
    // Evaluate strings into Golang
    literalRepo, err := jinja.EvaluateExpression(jinjaStr, nil)
    if err != nil {
        // Tolerate trailing commas inside list literals: ["a", "b", ]
        sanitized := strings.ReplaceAll(jinjaStr, ", ]", "]")
        sanitized = strings.ReplaceAll(sanitized, ",]", "]")
        literalRepo, err = jinja.EvaluateExpression(sanitized, nil)
    }
    if err == nil {
        switch literalRepo := literalRepo.(type) {
        case []string:
            return literalRepo, nil
        case []interface{}:
            var result []string
            for _, repo := range literalRepo {
                repoStr, ok := repo.(string)
                if ok {
                    result = append(result, repoStr)
                }
            }
            return result, nil
        }
    }
    return nil, fmt.Errorf("failed to evaluate Jinja string %q into a string list", jinjaStr)
}

// extractVariablesFromValue recursively extracts variables from a reflect.Value
func extractVariablesFromValue(val reflect.Value) ([]string, error) {
	if !val.IsValid() {
		return nil, nil
	}

	var vars []string

	// TODO: some values are a direct jinja expression (like when, failed_when, changed_when, etc.)
	// Instead of assuming that all values are Jinja strings, we should assign separate types to Jinja expressions.
	switch val.Kind() {
	case reflect.String:
		str := val.String()
		if str != "" {
			templateVars, err := jinja.ParseVariables(str)
			if err != nil {
				// If parsing fails, fall back to the existing regex-based approach
				templateVars = GetVariableUsageFromTemplate(str)
			}
			vars = append(vars, templateVars...)
		}

	case reflect.Struct:
		// Iterate through all fields of the struct
		for i := 0; i < val.NumField(); i++ {
			fieldVal := val.Field(i)
			if !fieldVal.CanInterface() {
				// Skip unexported fields
				continue
			}

			fieldVars, err := extractVariablesFromValue(fieldVal)
			if err != nil {
				return nil, err
			}
			vars = append(vars, fieldVars...)
		}

	case reflect.Slice, reflect.Array:
		// Iterate through all elements
		for i := 0; i < val.Len(); i++ {
			elemVal := val.Index(i)
			elemVars, err := extractVariablesFromValue(elemVal)
			if err != nil {
				return nil, err
			}
			vars = append(vars, elemVars...)
		}

	case reflect.Map:
		// Iterate through all map values (keys are typically not templated)
		iter := val.MapRange()
		for iter.Next() {
			mapVal := iter.Value()
			mapVars, err := extractVariablesFromValue(mapVal)
			if err != nil {
				return nil, err
			}
			vars = append(vars, mapVars...)
		}

	case reflect.Ptr:
		if !val.IsNil() {
			elemVars, err := extractVariablesFromValue(val.Elem())
			if err != nil {
				return nil, err
			}
			vars = append(vars, elemVars...)
		}

	case reflect.Interface:
		if !val.IsNil() {
			elemVars, err := extractVariablesFromValue(val.Elem())
			if err != nil {
				return nil, err
			}
			vars = append(vars, elemVars...)
		}

	// For other types (int, bool, float, etc.), no variables to extract
	default:
		// No variables in non-string types
	}

	return vars, nil
}
