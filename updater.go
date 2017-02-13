package main

import "log"
import "github.com/probandula/figlet4go"
import "runtime"
import "fmt"
import "io/ioutil"
import "strings"
import "os"
import "os/exec"
import "github.com/kardianos/osext"

import (
	"io"
	"net/http"
)

func downloadFile(filepath string, url string) (err error) {
    return nil

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Writer the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

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
		doCommand("xar", []string{"-xf", "go1.7.5.darwin-amd64.pkg"})
		doCommand("sh", []string{"-c", "cat com.googlecode.go.pkg/Payload | gunzip -dc | cpio -i"})
		os.Setenv("GOROOT", fmt.Sprintf("%v/usr/local/go/", folderPath))
		os.Setenv("PATH", fmt.Sprintf("%v/usr/local/go/bin/:%v", folderPath, os.Getenv("PATH")))
		doCommand("go", []string{"version"})
	}
}

func buildGo() {
	figlet("COMPILING GO")
	cwd, _ := os.Getwd()
	fmt.Println("I> Deleting directory golangCompiler")
	//doCommand("rm", []string{"-r", "golangCompiler"})
	doCommand("git", []string{"clone", "https://go.googlesource.com/go", "golangCompiler"})
	os.Chdir("golangCompiler/src")

	doCommand("git", []string{"checkout", "go1.7.5"})

	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		doCommand("bash", []string{"all.bash"})
		os.Chdir(cwd)
		os.Setenv("GOROOT", fmt.Sprintf("%v/golangCompiler/", cwd))
		os.Setenv("PATH", fmt.Sprintf("%v/golangCompiler/bin/:%v", cwd, os.Getenv("PATH")))
	} else {
		doCommand("all.bat", []string{})
		os.Chdir(cwd)
		os.Setenv("GOROOT", fmt.Sprintf("%v/golangCompiler/", cwd))
		os.Setenv("PATH", fmt.Sprintf("%v/golangCompiler/bin/:%v", cwd, os.Getenv("PATH")))
	}

}

func printEnv() {
	fmt.Println("I> GOROOT_BOOTSTRAP", os.Getenv("GOROOT_BOOTSTRAP"))
	fmt.Println("I> GOPATH", os.Getenv("GOPATH"))
	fmt.Println("I> PATH", os.Getenv("PATH"))
	fmt.Println("I> GOROOT", runtime.GOROOT())
}

func figlet(s string) string {
	ascii := figlet4go.NewAsciiRender()

	// Adding the colors to RenderOptions
	options := figlet4go.NewRenderOptions()
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		options.FontColor = []figlet4go.Color{
			// Colors can be given by default ansi color codes...
			figlet4go.ColorGreen,
			figlet4go.ColorYellow,
			figlet4go.ColorCyan,
			// ...or by an hex string...
			//figlet4go.NewTrueColorFromHexString("885DBA"),
			// ...or by an TrueColor object with rgb values
			//figlet4go.TrueColor{136, 93, 186},
		}
	}

	renderStr, _ := ascii.RenderOpts(s, options)
	return renderStr
}

func makeWith(optName, srcDir, libName string) string {
    return fmt.Sprintf("--with-%v=%v/%v", optName, srcDir, libName)
}

func makeOpt(optName, optVal string) string {
    return fmt.Sprintf("--%v=%v", optName, optVal)
}

func unTgzLib(lib string) {
	doCommand("tar", []string{"-xzvf", fmt.Sprintf("zips/%v.tar.gz", lib)})
}

func unBzLib(lib string) {
	doCommand("tar", []string{"-xjvf", fmt.Sprintf("zips/%v.tar.bz2", lib)})
}


func buildGcc(path string) {
	arch := "x86_64"
    targetDir := fmt.Sprintf("%v/fakeRoot", path)
    //srcDir := fmt.Sprintf("%v/src", path)
	os.Chdir(path)
	fmt.Println(figlet("GMP"))
	//doCommand("git", []string{"clone", "https://github.com/bw-oss/gmp"})
    gmpName := "gmp-6.1.2"
    mpfrName := "mpfr-3.1.5"
    mpcName := "mpc-1.0.3"
    gccName := "gcc-6.3.0"



    unBzLib(gmpName)
	os.Chdir(gmpName)
	//We need build= because the buck-toothed, cow-humping retards who use autoconf can't figure out I have the most common CPU architecture in the world
	//So glad you wrote that stupid little script to help!
	doCommand("./configure", []string{"--disable-shared", "--enable-static", makeOpt("prefix", targetDir), makeOpt("build", arch)})
	doCommand("make", []string{})
	doCommand("make", []string{"install"})

	//gcc-6.3.0.tar.gz  gmp-6.1.2.tar.bz2  isl-0.15.tar.bz2  mpc-0.8.1.tar.gz  mpfr-2.4.2.tar.bz2

	os.Chdir(path)

	fmt.Println(figlet("MPFR"))
	//doCommand("tar", []string{"-xjvf", "zips/mpfr-2.4.2.tar.bz2"})
	doCommand("tar", []string{"-xjvf", fmt.Sprintf("zips/%v.tar.bz2", mpfrName)})

	os.Chdir(mpfrName)
	doCommand("chmod", []string{"a+rwx", "configure"})
	//doCommand("./configure", []string{"--disable-shared", "--enable-static", makeWith("gmp", srcDir, "gmp-6.1.2"), makeWith("gmp", srcDir, "gmp-6.1.2"), makeOpt("prefix", targetDir)})
	doCommand("./configure", []string{"--disable-shared", "--enable-static", makeWith("gmp", targetDir, ""),makeOpt("prefix", targetDir)})
	doCommand("make", []string{})
	doCommand("make", []string{"install"})

	os.Chdir(path)

	fmt.Println(figlet("MPC"))
	unTgzLib(mpcName)
	os.Chdir(mpcName)
	doCommand("chmod", []string{"a+rwx", "configure"})
	doCommand("./configure", []string{"--disable-shared", "--enable-static", makeOpt("with-gmp", targetDir), makeOpt("with-mpfr", targetDir), makeOpt("prefix", targetDir)})
	doCommand("make", []string{})
	doCommand("make", []string{"install"})

	os.Chdir(path)

	fmt.Println(figlet("ISL"))
	doCommand("tar", []string{"-xjvf", "zips/isl-0.15.tar.bz2"})
	os.Chdir("isl-0.15")
	doCommand("chmod", []string{"a+rwx", "configure"})
	doCommand("./configure", []string{"--disable-shared", "--enable-static", makeOpt("with-gmp-prefix", targetDir), makeOpt("prefix", targetDir)})
	doCommand("make", []string{})
	doCommand("make", []string{"install"})

	os.Chdir(path)
	fmt.Println(figlet("GCC"))
	unTgzLib(gccName)
	os.Chdir(gccName)
	os.Chdir("gcc/objdir")
	doCommand(fmt.Sprintf("%v/%v/configure", path, gccName), []string{"--enable-languages=c,c++,go", "--disable-shared", "--enable-static", "--disable-multilib", "--disable-shared", "--enable-static", makeWith("gmp", targetDir, ""), makeWith("mpfr", targetDir, ""), makeWith("mpc", targetDir, ""), makeWith("isl", targetDir, ""), makeOpt("prefix", targetDir)})
	doCommand("make", []string{})
	doCommand("make", []string{"install"})

	os.Chdir(path)
    os.Exit(0)
}

func unSevenZ(file string) {
	doCommand("../7zip/7z.exe", []string{"x", file})
}

func main() {
	printEnv()
	folderPath, err := osext.ExecutableFolder()
	myDir := fmt.Sprintf("%v/goFiles", folderPath)
	zipsDir := fmt.Sprintf("%v/zips", folderPath)
	rootDir := fmt.Sprintf("%v/fakeRoot", folderPath)
	SzDir := fmt.Sprintf("%v7zip", folderPath)
	fmt.Println("I> Creating", myDir)
	os.Mkdir(myDir, os.ModeDir|0777)
	os.Mkdir(zipsDir, os.ModeDir|0777)
	os.Mkdir(rootDir, os.ModeDir|0777)
	os.Mkdir(SzDir, os.ModeDir|0777)
	if err != nil {
		os.Exit(1)
	}

	fmt.Println(figlet("GCC COMPILER"))
	//os.Exit(0)
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		buildGcc(folderPath)
		downloadFile("zips/gmp-6.1.2.tar.bz2", "https://gmplib.org/download/gmp/gmp-6.1.2.tar.bz2")
	} else {
		fmt.Println(figlet("DOWNLOADING"))
		downloadFile("zips/nuwen-14.1.7z", "https://nuwen.net/files/mingw/components-14.1.7z")
		downloadFile("zips/gcc-5.1.0-tdm64-1-core.zip", "https://kent.dl.sourceforge.net/project/tdm-gcc/TDM-GCC%205%20series/5.1.0-tdm64-1/gcc-5.1.0-tdm64-1-core.zip")
		downloadFile("zips/7z1604.exe", "http://www.7-zip.org/a/7z1604.exe")
		doCommand("zips/7z1604.exe", []string{"/S", fmt.Sprintf("/D=%v", SzDir)})
		doCommand("7zip/7z.exe", []string{"x", "zips/nuwen-14.1.7z"})
		os.Chdir("components-14.1")
		files, err := ioutil.ReadDir(".")
		if err != nil {
			log.Fatal(err)
		}

		for _, file := range files {
			if strings.HasSuffix(file.Name(), "7z") {
				unSevenZ(file.Name())
			}
		}
		os.Chdir(folderPath)
		os.Setenv("PATH", fmt.Sprintf("%v/components-14.1/bin/;%v", folderPath, os.Getenv("PATH")))
		printEnv()

	}
	fmt.Println(figlet("GO COMPILER"))
	os.Setenv("GOPATH", myDir)
	os.Setenv("GOROOT_BOOTSTRAP", runtime.GOROOT())
	printEnv()
	unPackGoMacOSX(folderPath)
	buildGo()
	printEnv()

	fmt.Println(figlet("LIBRARIES"))
	repos := loadRepos("libs")
	for _, v := range repos {
		installGithub(v)
	}

	fmt.Println(figlet("APPLICATIONS"))
	repos = loadRepos("apps")
	for _, v := range repos {
		installGithub(v)
	}

	fmt.Println(figlet("DO THIS"))
	fmt.Printf("\nNow set your path with one of the following commands\n\n")

	newPath := fmt.Sprintf("%v/usr/local/go/bin/", folderPath)
	fmt.Printf(setCommand(newPath))
	newPath = fmt.Sprintf("%v/bin/", myDir)
	fmt.Printf(setCommand(newPath))
	fmt.Printf("Job's a good'un, boss\n")
}

func setCommand(p string) string {
	return fmt.Sprintf("set -x PATH %v $PATH\nexport PATH=%v/:$PATH\n\n\n", p, p)
}
