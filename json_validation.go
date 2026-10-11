package main

import (
	"encoding/json"
	"fmt"
)

func validateJSONKeys(data []byte) error {
	if !json.Valid(data) {
		return fmt.Errorf("expected one valid JSON value")
	}
	_, err := checkJSONValue(data, 0)
	return err
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
