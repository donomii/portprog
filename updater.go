package main

import "runtime"
import "fmt"
import "io/ioutil"
import "strings"
import "os"
import "os/exec"
import "github.com/kardianos/osext"

func doCommand(cmd string, args []string) {
    fmt.Println("C>", cmd, args)
    out, err := exec.Command(cmd, args...).CombinedOutput()
    if err != nil {
        fmt.Fprintf(os.Stderr, "IO> %v", string(out))
        fmt.Fprintf(os.Stderr, "E> %v", err)
        //os.Exit(1)
    }
    if string(out) != "" {
        fmt.Fprintf(os.Stderr, "O> %v\n\n", string(out))
    }
}

func buildGithub(repo string) {
    cmd := "go"
    args := []string{"build", repo}
    fmt.Printf("I> Building %v\n", repo)
    doCommand(cmd, args)
}


func installGithub(repo string) {
    cmd := "go"
    args := []string{"get", "-u", repo}
    fmt.Printf("I> Installing %v\n", repo)
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
    if runtime.GOOS == "darwin" {
        doCommand("xar",  []string{ "-xf", "go1.7.5.darwin-amd64.pkg"} )
        doCommand("sh", []string{ "-c", "cat com.googlecode.go.pkg/Payload | gunzip -dc | cpio -i"} )
        os.Setenv("GOROOT", fmt.Sprintf("%v/usr/local/go/", folderPath))
        os.Setenv("PATH", fmt.Sprintf("%v/usr/local/go/bin/:%v", folderPath, os.Getenv("PATH")))
        doCommand("go", []string{ "version"} )
    }
}

func buildGo () {
    fmt.Println("I> Deleting directory golangCompiler")
    doCommand("rm", []string{"-r", "golangCompiler"})
    doCommand("git", []string{"clone", "https://go.googlesource.com/go", "golangCompiler"})
    os.Chdir("golangCompiler/src")
    
    doCommand("git", []string{"checkout", "go1.7.5"})
    doCommand("bash", []string{"all.bash"})
}

func main() {
    folderPath, err := osext.ExecutableFolder()
    myDir := fmt.Sprintf("%v/goFiles", folderPath)
    fmt.Println("I> Creating", myDir)
    os.Mkdir(myDir, os.ModeDir | 0777)
    if err != nil { os.Exit(1) }
    os.Setenv("GOPATH", myDir)
    fmt.Printf("I> Using GOPATH: %v\n", myDir)
    unPackGoMacOSX(folderPath)
    cwd, _ := os.Getwd()
    fmt.Println(os.Setenv("GOROOT_BOOTSTRAP", runtime.GOROOT()))
    //fmt.Println(os.Getenv("GOPATH"))
    //os.Exit(0)
    buildGo()
    os.Chdir(cwd)

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
