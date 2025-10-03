package config

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/go-viper/mapstructure/v2"
)

// Using custom Port type so we can use both string and int for port in the config.
type Port string

func (p Port) String() string {
	return string(p)
}

func PortDecodeHook() mapstructure.DecodeHookFuncType {
	return func(
		f reflect.Type,
		t reflect.Type,
		data any,
	) (any, error) {
		// Only process if target type is Port
		if t != reflect.TypeOf(Port("")) {
			return data, nil
		}

		switch v := data.(type) {
		case string:
			return Port(v), nil
		case int:
			return Port(strconv.Itoa(v)), nil
		case int64:
			return Port(strconv.FormatInt(v, 10)), nil
		case float64:
			// Handle case where YAML/JSON might parse integers as floats
			if v == float64(int(v)) {
				return Port(strconv.Itoa(int(v))), nil
			}
			return nil, fmt.Errorf("port must be an integer, got float: %v", v)
		default:
			return nil, fmt.Errorf("port must be a string or integer, got %T: %v", data, data)
		}
	}
}
