package main

import (
    "fmt"
    "os"
    "encoding/json"
    "io/ioutil"
)

type Package struct {
    Name, Zip, Url, Plan    string
    Branch string           // If this is git repository, what branch/tag do we check out before building?
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
