package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const maxJSONDepth = 32

var errStrictJSON = errors.New("malformed json")

// DecodeStrict parses exactly one JSON value, keeping numbers exact and refusing duplicate keys and deep nesting.
func DecodeStrict(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := readValue(dec, 0)
	if err != nil {
		return nil, errStrictJSON
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errStrictJSON
	}
	return v, nil
}

func readValue(dec *json.Decoder, depth int) (any, error) {
	if depth > maxJSONDepth {
		return nil, errStrictJSON
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	d, isDelim := tok.(json.Delim)
	if !isDelim {
		return tok, nil
	}
	switch d {
	case '{':
		obj := map[string]any{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, err
			}
			k, ok := kt.(string)
			if !ok {
				return nil, errStrictJSON
			}
			if _, dup := obj[k]; dup {
				return nil, errStrictJSON
			}
			if obj[k], err = readValue(dec, depth+1); err != nil {
				return nil, err
			}
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return obj, nil
	case '[':
		arr := []any{}
		for dec.More() {
			e, err := readValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, e)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return arr, nil
	}
	return nil, errStrictJSON
}
