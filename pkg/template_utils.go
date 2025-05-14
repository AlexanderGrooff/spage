package pkg

import (
	"fmt"
	"reflect"
)

// processRecursive is the core recursive function that creates a new reflect.Value
// based on originalVal, with string fields templated.
// It returns a new reflect.Value representing the copied and processed value, or an error.
func processRecursive(originalVal reflect.Value, closure *Closure) (reflect.Value, error) {
	if !originalVal.IsValid() {
		// Return the invalid value as is; the caller's .Set might handle or error.
		return originalVal, nil
	}

	switch originalVal.Kind() {
	case reflect.String:
		origStr := originalVal.String()
		templatedStr, err := TemplateString(origStr, closure)
		if err != nil {
			return reflect.Value{}, err // Propagate error
		}
		return reflect.ValueOf(templatedStr), nil

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

			processedFieldVal, err := processRecursive(originalFieldVal, closure)
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
		processedElemVal, err := processRecursive(elemVal, closure)
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
			processedElemVal, err := processRecursive(originalElemVal, closure)
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

			processedMapElemVal, err := processRecursive(originalMapElemVal, closure)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("failed to process map value for key %v: %w", key.Interface(), err)
			}
			if processedMapElemVal.IsValid() {
				newMapInstance.SetMapIndex(key, processedMapElemVal)
			}
		}
		return newMapInstance, nil

	default:
		// For basic types (int, bool, float, interface{}, etc.), return the original value.
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
	templatedStructVal, err := processRecursive(originalStructVal, closure)
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
