package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

func validateJSONKeys(data []byte) error {
	if !json.Valid(data) {
		var value json.RawMessage
		return json.Unmarshal(data, &value)
	}
	_, err := checkJSONValue(data, 0)
	return err
}

func validateDocumentFields(data json.RawMessage, shape reflect.Type) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) && shape.Kind() != reflect.Slice && shape.Kind() != reflect.Map {
		return fmt.Errorf("null is not a valid %s value", shape)
	}
	switch shape.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		known := make(map[string]reflect.Type)
		for index := 0; index < shape.NumField(); index++ {
			field := shape.Field(index)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" {
				name = field.Name
			}
			known[name] = field.Type
		}
		for name, value := range fields {
			field, exists := known[name]
			if !exists {
				return fmt.Errorf("unknown field %q in %s; field names are case-sensitive", name, shape.Name())
			}
			if err := validateDocumentFields(value, field); err != nil {
				return fmt.Errorf("field %s: %w", name, err)
			}
		}
	case reflect.Slice:
		var elements []json.RawMessage
		if err := json.Unmarshal(data, &elements); err != nil {
			return err
		}
		for index, value := range elements {
			if err := validateDocumentFields(value, shape.Elem()); err != nil {
				return fmt.Errorf("element %d: %w", index, err)
			}
		}
	case reflect.Map:
		var values map[string]json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		for name, value := range values {
			if err := validateDocumentFields(value, shape.Elem()); err != nil {
				return fmt.Errorf("map key %s: %w", name, err)
			}
		}
	}
	return nil
}

func jsonWhitespace(data []byte, offset int) int {
	for offset < len(data) {
		switch data[offset] {
		case ' ', '\t', '\r', '\n':
			offset++
		default:
			return offset
		}
	}
	return offset
}

func jsonStringEnd(data []byte, offset int) int {
	for index := offset + 1; index < len(data); index++ {
		if data[index] == '\\' {
			index++
		} else if data[index] == '"' {
			return index + 1
		}
	}
	return len(data)
}

func checkJSONValue(data []byte, offset int) (int, error) {
	offset = jsonWhitespace(data, offset)
	switch data[offset] {
	case '{':
		return checkJSONObject(data, offset)
	case '[':
		offset = jsonWhitespace(data, offset+1)
		for data[offset] != ']' {
			end, err := checkJSONValue(data, offset)
			if err != nil {
				return end, err
			}
			offset = jsonWhitespace(data, end)
			if data[offset] == ',' {
				offset = jsonWhitespace(data, offset+1)
			}
		}
		return offset + 1, nil
	case '"':
		return jsonStringEnd(data, offset), nil
	default:
		for offset < len(data) {
			switch data[offset] {
			case ',', '}', ']', ' ', '\n', '\r', '\t':
				return offset, nil
			default:
				offset++
			}
		}
		return offset, nil
	}
}

func checkJSONObject(data []byte, offset int) (int, error) {
	seen := make(map[string]bool)
	offset = jsonWhitespace(data, offset+1)
	for data[offset] != '}' {
		end := jsonStringEnd(data, offset)
		var key string
		if err := json.Unmarshal(data[offset:end], &key); err != nil {
			return offset, fmt.Errorf("decode JSON object key at byte %d: %w", offset, err)
		}
		if seen[key] {
			return offset, fmt.Errorf("duplicate JSON object key %q at byte %d", key, offset)
		}
		seen[key] = true
		valueOffset := jsonWhitespace(data, end) + 1
		end, err := checkJSONValue(data, valueOffset)
		if err != nil {
			return end, err
		}
		offset = jsonWhitespace(data, end)
		if data[offset] == ',' {
			offset = jsonWhitespace(data, offset+1)
		}
	}
	return offset + 1, nil
}
