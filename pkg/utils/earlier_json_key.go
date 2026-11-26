package utils

import "encoding/json"

// AdoptEarlierJSONKey decodes the value of an object's earlier key into
// `into` when the object has no current key. Saved data written before a key
// was renamed keeps reading after a type's own decode has run.
func AdoptEarlierJSONKey(data []byte, current, earlier string, into interface{}) error {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil
	}
	value, ok := keys[earlier]
	if _, has := keys[current]; has || !ok {
		return nil
	}
	return json.Unmarshal(value, into)
}
