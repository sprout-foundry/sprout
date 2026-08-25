package tools

import "bytes"

// JSON helpers (package-private, not exported).
// These types wrap the jsonEnc indirection layer so that
// structuredEncoder/structuredDecoder can dispatch to JSON
// without importing encoding/json directly.

type jsonEncoder struct {
	enc *jsonEnc
}

func newJSONEncoder(buf *bytes.Buffer) *jsonEncoder {
	return &jsonEncoder{enc: newJSONEnc(buf)}
}

func (e *jsonEncoder) Encode(v interface{}) error {
	return e.enc.Encode(v)
}

type jsonDecoder struct {
	data []byte
}

func newJSONDecoder(data []byte) *jsonDecoder {
	return &jsonDecoder{data: data}
}

func (d *jsonDecoder) Decode(v interface{}) error {
	return doJSONUnmarshal(d.data, v)
}
