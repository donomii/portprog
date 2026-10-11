package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func loadPackage(filename string) (Package, error) {
	file, err := os.Open(filename)
	if err != nil {
		return Package{}, fmt.Errorf("read package %q: %w", filename, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var recipe Package
	if err := decoder.Decode(&recipe); err != nil {
		return Package{}, fmt.Errorf("decode package %q: %w", filename, err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Package{}, fmt.Errorf("decode package %q: expected one JSON object, received trailing JSON", filename)
		}
		return Package{}, fmt.Errorf("decode package %q trailing content: %w", filename, err)
	}
	if err := validatePackage(recipe); err != nil {
		return Package{}, fmt.Errorf("validate package %q: %w", filename, err)
	}
	return recipe, nil
}
