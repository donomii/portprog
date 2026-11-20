package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

type jsonKeyScanner struct {
	data []byte
	pos  int
}

const maximumJSONDepth = 256

func newStrictJSONDecoder(data []byte, source string) (*json.Decoder, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("validate JSON %q: expected valid UTF-8 text", source)
	} else if err := rejectDuplicateJSONKeys(data, source); err != nil {
		return nil, err
	} else {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		return decoder, nil
	}
}

func requireJSONEnd(decoder *json.Decoder, source string) error {
	var trailing struct{}
	err := decoder.Decode(&trailing)
	if err == io.EOF {
		return nil
	} else if err == nil {
		return fmt.Errorf("decode JSON %q: expected one document, got trailing JSON data", source)
	} else {
		return fmt.Errorf("decode JSON %q: expected the end of one document, got error: %w", source, err)
	}
}

func rejectDuplicateJSONKeys(data []byte, source string) error {
	scanner := jsonKeyScanner{data: data}
	if err := scanner.parseValue(); err != nil {
		return fmt.Errorf("validate JSON %q: %w", source, err)
	} else {
		scanner.skipSpace()
		if scanner.pos != len(scanner.data) {
			return fmt.Errorf("validate JSON %q: expected end of document at byte %d", source, scanner.pos)
		} else {
			return nil
		}
	}
}

func (scanner *jsonKeyScanner) parseValue() error {
	return scanner.parseValueAtDepth(0)
}

func (scanner *jsonKeyScanner) parseValueAtDepth(depth int) error {
	scanner.skipSpace()
	if depth > maximumJSONDepth {
		return fmt.Errorf("expected JSON nesting at most %d levels at byte %d", maximumJSONDepth, scanner.pos)
	} else if scanner.pos >= len(scanner.data) {
		return fmt.Errorf("expected a JSON value at byte %d, got end of input", scanner.pos)
	} else {
		switch scanner.data[scanner.pos] {
		case '{':
			return scanner.parseObject(depth)
		case '[':
			return scanner.parseArray(depth)
		case '"':
			_, err := scanner.parseString()
			return err
		case 't':
			return scanner.parseLiteral("true")
		case 'f':
			return scanner.parseLiteral("false")
		case 'n':
			return scanner.parseLiteral("null")
		default:
			return scanner.parseNumber()
		}
	}
}

func (scanner *jsonKeyScanner) parseObject(depth int) error {
	scanner.pos++
	scanner.skipSpace()
	keys := make(map[string]struct{})
	if scanner.consume('}') {
		return nil
	} else {
		for {
			key, err := scanner.parseString()
			if err != nil {
				return err
			} else {
				if _, duplicate := keys[key]; duplicate {
					return fmt.Errorf("expected unique object fields, got duplicate key %q", key)
				} else {
					keys[key] = struct{}{}
				}
			}
			scanner.skipSpace()
			if !scanner.consume(':') {
				return fmt.Errorf("expected ':' after object key %q at byte %d", key, scanner.pos)
			} else if valueErr := scanner.parseValueAtDepth(depth + 1); valueErr != nil {
				return valueErr
			} else {
				scanner.skipSpace()
				if scanner.consume('}') {
					return nil
				} else if scanner.consume(',') {
					scanner.skipSpace()
					continue
				} else {
					return fmt.Errorf("expected ',' or '}' at byte %d", scanner.pos)
				}
			}
		}
	}
}

func (scanner *jsonKeyScanner) parseArray(depth int) error {
	scanner.pos++
	scanner.skipSpace()
	if scanner.consume(']') {
		return nil
	} else {
		for {
			if err := scanner.parseValueAtDepth(depth + 1); err != nil {
				return err
			} else {
				scanner.skipSpace()
				if scanner.consume(']') {
					return nil
				} else if scanner.consume(',') {
					scanner.skipSpace()
					continue
				} else {
					return fmt.Errorf("expected ',' or ']' at byte %d", scanner.pos)
				}
			}
		}
	}
}

func (scanner *jsonKeyScanner) parseString() (string, error) {
	scanner.skipSpace()
	if scanner.pos >= len(scanner.data) || scanner.data[scanner.pos] != '"' {
		return "", fmt.Errorf("expected a JSON string at byte %d", scanner.pos)
	} else {
		start := scanner.pos
		scanner.pos++
		escaped := false
		for scanner.pos < len(scanner.data) {
			current := scanner.data[scanner.pos]
			scanner.pos++
			if escaped {
				escaped = false
				continue
			} else if current == '\\' {
				escaped = true
				continue
			} else if current == '"' {
				var value string
				if err := json.Unmarshal(scanner.data[start:scanner.pos], &value); err != nil {
					return "", fmt.Errorf("decode JSON string at byte %d: %w", start, err)
				} else {
					return value, nil
				}
			} else if current < 0x20 {
				return "", fmt.Errorf("expected printable JSON string data at byte %d", scanner.pos-1)
			} else {
				continue
			}
		}
		return "", fmt.Errorf("expected closing quote for JSON string at byte %d", start)
	}
}

func (scanner *jsonKeyScanner) parseLiteral(literal string) error {
	end := scanner.pos + len(literal)
	if end > len(scanner.data) || string(scanner.data[scanner.pos:end]) != literal {
		return fmt.Errorf("expected JSON literal %q at byte %d", literal, scanner.pos)
	} else {
		scanner.pos = end
		return nil
	}
}

func (scanner *jsonKeyScanner) parseNumber() error {
	start := scanner.pos
	if scanner.consume('-') {
		scanner.skipNumberDigits()
	} else {
		scanner.skipNumberDigits()
	}
	scanner.parseNumberFraction()
	scanner.parseNumberExponent()
	if scanner.pos == start {
		return fmt.Errorf("expected a JSON value at byte %d", start)
	} else {
		var number json.Number
		if err := json.Unmarshal(scanner.data[start:scanner.pos], &number); err != nil {
			return fmt.Errorf("decode JSON number at byte %d: %w", start, err)
		} else {
			_, floatErr := strconv.ParseFloat(number.String(), 64)
			if floatErr != nil {
				return fmt.Errorf("decode JSON number %q at byte %d: %w", number.String(), start, floatErr)
			} else {
				return nil
			}
		}
	}
}

func (scanner *jsonKeyScanner) parseNumberFraction() {
	if scanner.consume('.') {
		scanner.skipNumberDigits()
	} else {
		return
	}
}

func (scanner *jsonKeyScanner) parseNumberExponent() {
	if scanner.consume('e') || scanner.consume('E') {
		if scanner.consume('+') || scanner.consume('-') {
			scanner.skipNumberDigits()
		} else {
			scanner.skipNumberDigits()
		}
	} else {
		return
	}
}

func (scanner *jsonKeyScanner) skipNumberDigits() {
	for scanner.pos < len(scanner.data) && scanner.data[scanner.pos] >= '0' && scanner.data[scanner.pos] <= '9' {
		scanner.pos++
	}
}

func (scanner *jsonKeyScanner) skipSpace() {
	for scanner.pos < len(scanner.data) {
		switch scanner.data[scanner.pos] {
		case ' ', '\t', '\n', '\r':
			scanner.pos++
		default:
			return
		}
	}
}

func (scanner *jsonKeyScanner) consume(expected byte) bool {
	if scanner.pos < len(scanner.data) && scanner.data[scanner.pos] == expected {
		scanner.pos++
		return true
	} else {
		return false
	}
}
