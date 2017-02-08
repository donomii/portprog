package main

import "fmt"
import "io/ioutil"
import "strings"
import "os"
import "os/exec"
import "github.com/kardianos/osext"

func doCommand(cmd string, args []string) {
    out, err := exec.Command(cmd, args...).CombinedOutput()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Output: %v", string(out))
        fmt.Fprintf(os.Stderr, "Error: %v", err)
        //os.Exit(1)
    }
    if string(out) != "" {
        fmt.Fprintf(os.Stderr, "Output: %v\n\n", string(out))
    }
}

func buildGithub(repo string) {
    cmd := "go"
    args := []string{"build", repo}
    fmt.Printf("Building %v\n", repo)
    doCommand(cmd, args)
}


func installGithub(repo string) {
    cmd := "go"
    args := []string{"get", "-u", repo}
    fmt.Printf("Installing %v\n", repo)
    doCommand(cmd, args)
}

func loadRepos(filename string) []string {
    content, err := ioutil.ReadFile(filename)
    if err != nil {
        //Do something
    }
    lines := strings.Split(string(content), "\n")
    return lines
}

func unPackGoMacOSX(folderPath string) {
    doCommand("xar",  []string{ "-xf", "go1.7.5.darwin-amd64.pkg"} )
    doCommand("sh", []string{ "-c", "cat com.googlecode.go.pkg/Payload | gunzip -dc | cpio -i"} )
    os.Setenv("GOROOT", fmt.Sprintf("%v/usr/local/go/", folderPath))
    os.Setenv("PATH", fmt.Sprintf("%v/usr/local/go/bin/:%v", folderPath, os.Getenv("PATH")))
    doCommand("go", []string{ "version"} )
}

func main() {
    folderPath, err := osext.ExecutableFolder()
    myDir := fmt.Sprintf("%v/goFiles", folderPath)
    os.Mkdir(myDir, os.ModeDir | 0777)
    if err != nil { os.Exit(1) }
    os.Setenv("GOPATH", myDir)
    unPackGoMacOSX(folderPath)

    repos := loadRepos("libs")
    for _,v := range repos {
        installGithub( v )
    }

    repos = loadRepos("apps")
    for _,v := range repos {
        installGithub( v )
    }

    fmt.Printf("\nNow set your path with one of the following commands\n\n")

    newPath := fmt.Sprintf("%v/usr/local/go/bin/", folderPath)
    fmt.Printf(setCommand(newPath))
    newPath = fmt.Sprintf("%v/bin/", myDir)
    fmt.Printf(setCommand(newPath))
    fmt.Printf("Job's a good'un, boss\n")
}


func setCommand(p string) string {
    return fmt.Sprintf("set -x PATH %v $PATH\nexport PATH=$PATH:%v/bin/\n\n\n", p, p)
}
