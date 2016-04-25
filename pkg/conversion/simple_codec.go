package conversion

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/golang/glog"
)

type SimpleCodec struct {
	typeMap map[string]reflect.Type
}

func NewSimpleCodec() *SimpleCodec {
	return &SimpleCodec{
		typeMap: map[string]reflect.Type{},
	}
}

func (c *SimpleCodec) Decode(data []byte) (interface{}, error) {
	obj, err := c.DecodeToObject(data)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func (c *SimpleCodec) DecodeInto(data []byte, obj interface{}) error {
	return nil
}

func (c *SimpleCodec) Encode(obj interface{}) (data []byte, err error) {
	// To add metadata, do some simple surgery on the JSON.
	glog.V(5).Infof("Encoding %v", obj)
	data, err = json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return
}

func (c *SimpleCodec) DataKind(data []byte) (string, error) {
	findKind := struct {
		Kind string `json:"kind,omitempty"`
	}{}
	err := json.Unmarshal(data, &findKind)
	if err != nil {
		return "", fmt.Errorf("Couldn't get kind; json parse error: %v", err)
	}
	return findKind.Kind, nil
}

func (c *SimpleCodec) DecodeToObject(data []byte) (obj interface{}, err error) {
	kind, err := c.DataKind(data)
	if err != nil {
		return
	}
	if kind == "" {
		return nil, fmt.Errorf("kind not set in '%s'", string(data))
	}
	obj, err = c.NewObject(kind)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, obj); err != nil {
		return nil, err
	}
	return
}

// AddKnownTypes registers all types passed in 'types'.
// Encode() will refuse objects unless their type has been registered with AddKnownTypes.
// All objects passed to types should be pointers to structs. The name that go reports for
// the struct becomes the "kind" field when encoding.
func (c *SimpleCodec) AddKnownTypes(types ...interface{}) {
	for _, obj := range types {
		t := reflect.TypeOf(obj)
		if t.Kind() != reflect.Ptr {
			panic("All types must be pointers to structs.")
		}
		t = t.Elem()
		if t.Kind() != reflect.Struct {
			panic("All types must be pointers to structs.")
		}
		c.typeMap[t.Name()] = t
	}
}

// NewObject returns a new object of the given kind,
// or an error if it hasn't been registered.
func (c *SimpleCodec) NewObject(kind string) (interface{}, error) {
	if t, ok := c.typeMap[kind]; ok {
		return reflect.New(t).Interface(), nil
	}
	return nil, fmt.Errorf("The kind %s is not registered in vmt service", kind)
}

// EnforcePtr ensures that obj is a pointer of some sort. Returns a reflect.Value
// of the dereferenced pointer, ensuring that it is settable/addressable.
// Returns an error if this is not possible.
func EnforcePtr(obj interface{}) (reflect.Value, error) {
	v := reflect.ValueOf(obj)
	if v.Kind() != reflect.Ptr {
		if v.Kind() == reflect.Invalid {
			return reflect.Value{}, fmt.Errorf("expected pointer, but got invalid kind")
		}
		return reflect.Value{}, fmt.Errorf("expected pointer, but got %v type", v.Type())
	}
	if v.IsNil() {
		return reflect.Value{}, fmt.Errorf("expected pointer, but got nil")
	}
	return v.Elem(), nil
}
