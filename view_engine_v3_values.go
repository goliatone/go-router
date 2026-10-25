package router

import (
	"fmt"
	"reflect"

	p6 "github.com/flosch/pongo2/v6"
	p7 "github.com/flosch/pongo2/v7"
)

var (
	fiberV3ForeignValueType = reflect.TypeFor[p6.Value]()
	fiberV3ValueType        = reflect.TypeFor[*p7.Value]()
	fiberV3ErrorType        = reflect.TypeFor[error]()
)

func fiberV3ValueError(path, reason string) error {
	return fmt.Errorf("fiber v3 views: %s: %s; migrate helpers/data to plain Go values or pongo2/v7.Value", path, reason)
}

// Copy view containers and wrap callable results before pongo2 can interpret
// them. A v6 Value is otherwise an opaque, truthy Stringer in the v7 evaluator.
// Never infer its private safety flag or mutate either runtime's globals.
func fiberV3ViewValue(value any, path string) (any, error) {
	v, err := fiberV3NormalizeValue(reflect.ValueOf(value), path, 0, true)
	if err != nil || !v.IsValid() {
		return nil, err
	}
	return v.Interface(), nil
}

func fiberV3NormalizeValue(v reflect.Value, path string, depth int, adapt bool) (reflect.Value, error) {
	if !v.IsValid() {
		return v, nil
	}
	if depth > 64 {
		return reflect.Value{}, fiberV3ValueError(path, "cyclic or excessively deep view data")
	}
	t := v.Type()
	if t == fiberV3ForeignValueType || t == reflect.PointerTo(fiberV3ForeignValueType) {
		return reflect.Value{}, fiberV3ValueError(path, "pongo2/v6.Value is incompatible with the scoped runtime")
	}
	if t == fiberV3ValueType {
		if !v.IsNil() {
			if _, err := fiberV3NormalizeValue(reflect.ValueOf(v.Interface().(*p7.Value).Interface()), path, depth+1, false); err != nil {
				return reflect.Value{}, err
			}
		}
		return v, nil // Preserve the native runtime's safe/unsafe distinction.
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return v, nil
		}
		return fiberV3NormalizeValue(v.Elem(), path, depth+1, adapt)
	}
	// Opaque models keep their type, so every reachable method result must be
	// closed data. Those calls are made by pongo2 and cannot be wrapped here.
	if fiberV3DynamicMethods(t, make(map[reflect.Type]bool)) {
		return reflect.Value{}, fiberV3ValueError(path, "dynamic or foreign model methods require an explicit view helper")
	}

	switch v.Kind() {
	case reflect.Func:
		if !adapt {
			return reflect.Value{}, fiberV3ValueError(path, "callable fields/boxed callables require an explicit view helper")
		}
		if v.IsNil() {
			return reflect.Value{}, fiberV3ValueError(path, "nil template helper")
		}
		for i := 0; i < t.NumIn(); i++ {
			if fiberV3ContainsForeignType(t.In(i), map[reflect.Type]bool{}) {
				return reflect.Value{}, fiberV3ValueError(path, "helper accepts pongo2/v6.Value")
			}
		}
		for i := 0; i < t.NumOut(); i++ {
			if fiberV3ContainsForeignType(t.Out(i), map[reflect.Type]bool{}) {
				return reflect.Value{}, fiberV3ValueError(path, "helper returns pongo2/v6.Value")
			}
		}
		if t.NumOut() < 1 || t.NumOut() > 2 || t.NumOut() == 2 && !t.Out(1).Implements(fiberV3ErrorType) {
			return reflect.Value{}, fiberV3ValueError(path, "helper must return a value or (value, error)")
		}
		inputs := make([]reflect.Type, t.NumIn())
		for i := range inputs {
			inputs[i] = t.In(i)
		}
		wrapper := reflect.FuncOf(inputs, []reflect.Type{fiberV3ValueType, fiberV3ErrorType}, t.IsVariadic())
		return reflect.MakeFunc(wrapper, func(args []reflect.Value) []reflect.Value {
			var result []reflect.Value
			if t.IsVariadic() {
				result = v.CallSlice(args)
			} else {
				result = v.Call(args)
			}
			failure := func(err error) []reflect.Value {
				return []reflect.Value{reflect.Zero(fiberV3ValueType), reflect.ValueOf(err)}
			}
			if len(result) == 2 {
				err, _ := result[1].Interface().(error)
				if err != nil {
					return failure(err)
				}
			}
			normalized, err := fiberV3NormalizeValue(result[0], path+" result", 0, true)
			if err != nil {
				return failure(err)
			}
			var output any
			if normalized.IsValid() {
				output = normalized.Interface()
			}
			boxed, ok := output.(*p7.Value)
			if !ok {
				boxed = p7.AsValue(output)
			}
			return []reflect.Value{reflect.ValueOf(boxed), reflect.Zero(fiberV3ErrorType)}
		}), nil
	case reflect.Map:
		if v.IsNil() {
			return v, nil
		}
		out := reflect.MakeMapWithSize(t, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			if _, err := fiberV3NormalizeValue(iter.Key(), path+" key", depth+1, false); err != nil {
				return reflect.Value{}, err
			}
			item, err := fiberV3NormalizeValue(iter.Value(), path+" item", depth+1, adapt)
			if err != nil {
				return reflect.Value{}, err
			}
			if !item.IsValid() {
				item = reflect.Zero(t.Elem())
			}
			if !item.Type().AssignableTo(t.Elem()) {
				return reflect.Value{}, fiberV3ValueError(path, "adapted helper requires a map/slice of any")
			}
			out.SetMapIndex(iter.Key(), item)
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return v, nil
		}
		var out reflect.Value
		if v.Kind() == reflect.Slice {
			out = reflect.MakeSlice(t, v.Len(), v.Len())
		} else {
			out = reflect.New(t).Elem()
		}
		for i := 0; i < v.Len(); i++ {
			item, err := fiberV3NormalizeValue(v.Index(i), path+" item", depth+1, adapt)
			if err != nil {
				return reflect.Value{}, err
			}
			if !item.IsValid() {
				item = reflect.Zero(t.Elem())
			}
			if !item.Type().AssignableTo(t.Elem()) {
				return reflect.Value{}, fiberV3ValueError(path, "adapted helper requires a map/slice of any")
			}
			out.Index(i).Set(item)
		}
		return out, nil
	case reflect.Pointer, reflect.Struct:

		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return v, nil
			}
			item, err := fiberV3NormalizeValue(v.Elem(), path, depth+1, false)
			if err != nil {
				return reflect.Value{}, err
			}
			// Validate rather than copy opaque objects (including self-references).
			_ = item
		} else {
			for i := 0; i < v.NumField(); i++ {
				if t.Field(i).IsExported() {
					if _, err := fiberV3NormalizeValue(v.Field(i), path+"."+t.Field(i).Name, depth+1, false); err != nil {
						return reflect.Value{}, err
					}
				}
			}
		}
	}
	return v, nil
}

func fiberV3ContainsForeignType(t reflect.Type, seen map[reflect.Type]bool) bool {
	if t == fiberV3ForeignValueType || t == reflect.PointerTo(fiberV3ForeignValueType) {
		return true
	}
	if seen[t] {
		return false
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return fiberV3ContainsForeignType(t.Elem(), seen)
	case reflect.Map:
		return fiberV3ContainsForeignType(t.Key(), seen) || fiberV3ContainsForeignType(t.Elem(), seen)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).IsExported() && fiberV3ContainsForeignType(t.Field(i).Type, seen) {
				return true
			}
		}
	case reflect.Func:
		for i := 0; i < t.NumIn(); i++ {
			if fiberV3ContainsForeignType(t.In(i), seen) {
				return true
			}
		}
		for i := 0; i < t.NumOut(); i++ {
			if fiberV3ContainsForeignType(t.Out(i), seen) {
				return true
			}
		}
	}
	return false
}

func fiberV3DynamicType(t reflect.Type, seen map[reflect.Type]bool) bool {
	if t == fiberV3ValueType || t == fiberV3ForeignValueType || t == reflect.PointerTo(fiberV3ForeignValueType) {
		return true
	}
	if seen[t] {
		return false
	}
	seen[t] = true
	if fiberV3DynamicMethods(t, seen) {
		return true
	}
	switch t.Kind() {
	case reflect.Interface, reflect.Func:
		return true
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return fiberV3DynamicType(t.Elem(), seen)
	case reflect.Map:
		return fiberV3DynamicType(t.Key(), seen) || fiberV3DynamicType(t.Elem(), seen)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).IsExported() && fiberV3DynamicType(t.Field(i).Type, seen) {
				return true
			}
		}
	}
	return false
}

// Follow method results as well as fields: a closed-looking struct can return
// another object with a dynamic getter. The shared visited set terminates
// recursive type graphs; no application method is invoked during validation.
func fiberV3DynamicMethods(t reflect.Type, seen map[reflect.Type]bool) bool {
	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i).Type
		for j := 1; j < method.NumIn(); j++ {
			if fiberV3ContainsForeignType(method.In(j), make(map[reflect.Type]bool)) {
				return true
			}
		}
		for j := 0; j < method.NumOut(); j++ {
			// Only this position is consumed as error handling by pongo2. A
			// sole error result is ordinary view data with an unknown concrete
			// method set, and must not be exempt from the boundary.
			if j == 1 && method.NumOut() == 2 && method.Out(j).Implements(fiberV3ErrorType) {
				continue
			}
			if fiberV3DynamicType(method.Out(j), seen) {
				return true
			}
		}
	}
	return false
}
