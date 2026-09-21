package types

import "encoding/json"

// JSONString is a string containing JSON encoded data, it prevents further json encoding when used inside a struct that gets encoded
// cant' use json.RawMessage because of:sql: Scan error on column index 1, name "processes": unsupported Scan, storing driver.Value type string into type *json.RawMessage
type JSONString string

func (js JSONString) MarshalJSON() ([]byte, error) {
	if js == "" {
		return []byte("null"), nil
	}
	// The values that reach this type are agent-supplied and are stored
	// verbatim, so "is this JSON?" is not a safe assumption. Splicing a
	// non-JSON blob in raw makes encoding/json fail on the WHOLE response, so
	// one poisoned measurement row took out every monitoring request for that
	// client for as long as the row was inside the requested window -- and the
	// agent could keep it there. A row the server cannot represent becomes
	// null rather than a 500 for everything around it.
	if !json.Valid([]byte(js)) {
		return []byte("null"), nil
	}
	return []byte(js), nil
}

func (js *JSONString) UnmarshalJSON(data []byte) error {
	*js = JSONString(data)
	return nil
}
