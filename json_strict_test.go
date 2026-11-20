package main

import (
	"strings"
	"testing"
)

func TestStrictJSONRejectsExcessiveNesting(t *testing.T) {
	data := []byte(strings.Repeat("[", maximumJSONDepth+2) + "0" + strings.Repeat("]", maximumJSONDepth+2))
	if err := rejectDuplicateJSONKeys(data, "deep fixture"); err == nil {
		t.Fatal("validate excessively nested JSON: expected rejection, got success")
	} else if !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("validate excessively nested JSON: expected nesting explanation, got %v", err)
	} else {
		return
	}
}

func TestStrictJSONRejectsInvalidUTF8(t *testing.T) {
	data := []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}
	if _, err := newStrictJSONDecoder(data, "invalid UTF-8 fixture"); err == nil {
		t.Fatal("validate invalid UTF-8 JSON: expected rejection, got success")
	} else {
		return
	}
}
