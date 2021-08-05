package converter

import "reflect"

type Converter interface {
	CovertStringToInterface(value string) (interface{}, error)
	CovertStringToPrimitiveTypeAndSetField(field reflect.Value, value string, isPtr bool) error
}

func StringConverterFactory(vType string) Converter {
	switch vType {
	case "string":
		return StringConverter{}
	case "int32":
		return Int32Converter{}
	case "int64":
		return Int64Converter{}
	case "float32":
		return Float32Converter{}
	case "float64":
		return Float32Converter{}
	case "bool":
		return BooleanConverter{}
	default:
		return nil
	}
}
