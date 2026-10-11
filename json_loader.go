package main

import "fmt"

func loadPackage(filename string) (Package, error) {
	recipe, err := readJSONDocument[Package](filename)
	if err != nil {
		return Package{}, err
	}
	if err := validatePackage(recipe); err != nil {
		return Package{}, fmt.Errorf("validate package %q: %w", filename, err)
	}
	return recipe, nil
}
