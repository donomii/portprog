package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
)

type Package struct {
	Name            string   //Name of the package
	Zip             string   //Full archive name, including extension
	Url             string   //URL to download the archive from
	Fetch           string   //Fetch method (e.g. wget, curl, git, svn, etc)
	Plan            string   //Build plan (e.g. make, cmake, etc)
	Branch, Command string   // If this is git repository, what branch/tag do we check out before building?
	BinDir, LibDir  string   //Where do we find the binary files after the build, relative to the top level directory of the installed package?
	BinDirs         []string //If there are multiple directories to add to the path.  Both BinDir and BinDirs are added to the path.
	Deletes         []string
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
