package conf

import (
	"fmt"
	"reflect"
	"strconv"
	"time"
)

func setDefaults(target any) error {
	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("configuration must be a non-nil pointer to a struct")
	}
	return setDefaultsRecursive(value.Elem(), "")
}

func setDefaultsRecursive(value reflect.Value, prefix string) error {
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		if !field.CanSet() {
			continue
		}
		fieldType := value.Type().Field(index)
		name := prefix + fieldType.Name
		if field.Kind() == reflect.Struct {
			if err := setDefaultsRecursive(field, name+"."); err != nil {
				return err
			}
		}
		if defaultValue := fieldType.Tag.Get("default"); field.IsZero() && defaultValue != "" {
			if err := setDefaultValue(field, defaultValue); err != nil {
				return fmt.Errorf("default for %s: %w", name, err)
			}
		}
	}
	return nil
}

func setDefaultValue(field reflect.Value, text string) error {
	if field.Type() == reflect.TypeOf(time.Duration(0)) {
		duration, err := time.ParseDuration(text)
		if err != nil {
			return err
		}
		field.SetInt(int64(duration))
		return nil
	}
	switch field.Kind() {
	case reflect.String:
		field.SetString(text)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(text, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		parsed, err := strconv.ParseUint(text, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(text, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetFloat(parsed)
	case reflect.Bool:
		parsed, err := strconv.ParseBool(text)
		if err != nil {
			return err
		}
		field.SetBool(parsed)
	default:
		return fmt.Errorf("unsupported type %s", field.Type())
	}
	return nil
}
