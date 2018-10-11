package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
)

type Package struct {
	Name, Zip, Url, Fetch, Plan string
	Branch, Command             string // If this is git repository, what branch/tag do we check out before building?
	BinDir, LibDir				string	//Where do we find the binary files, relative to the top level directory of the installed package?
	BinDirs, Deletes []string
}

func LoadJSON(filename string) Package {
	file, e := ioutil.ReadFile(filename)
	if e != nil {
		fmt.Printf("File error: %v\n", e)
		os.Exit(1)
	}
	fmt.Printf("%s\n", string(file))

	//m := new(Dispatch)
	//var m interface{}
	var retType Package
	json.Unmarshal(file, &retType)
	fmt.Printf("Results: %v\n", retType)
	return retType
}
