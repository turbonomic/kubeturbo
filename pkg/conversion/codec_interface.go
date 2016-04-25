package conversion

// Decoder defines methods for deserializing API objects into a given type
type Decoder interface {
	Decode(data []byte) (interface{}, error)
	// DecodeToVersion(data []byte, version string) (Object, error)
	DecodeInto(data []byte, obj interface{}) error
	// DecodeIntoWithSpecifiedVersionKind(data []byte, obj Object, kind, version string) error
}

// Encoder defines methods for serializing API objects into bytes
type Encoder interface {
	Encode(obj interface{}) (data []byte, err error)
}

// Codec defines methods for serializing and deserializing API objects.
type Codec interface {
	Decoder
	Encoder
}
