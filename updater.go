package main

import "github.com/probandula/figlet4go"
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
		doCommand("xar", []string{"-xf", "go1.7.5.darwin-amd64.pkg"})
		doCommand("sh", []string{"-c", "cat com.googlecode.go.pkg/Payload | gunzip -dc | cpio -i"})
		os.Setenv("GOROOT", fmt.Sprintf("%v/usr/local/go/", folderPath))
		os.Setenv("PATH", fmt.Sprintf("%v/usr/local/go/bin/:%v", folderPath, os.Getenv("PATH")))
		doCommand("go", []string{"version"})
	}
}

func buildGo() {
	cwd, _ := os.Getwd()
	fmt.Println("I> Deleting directory golangCompiler")
	//doCommand("rm", []string{"-r", "golangCompiler"})
	doCommand("git", []string{"clone", "https://go.googlesource.com/go", "golangCompiler"})
	os.Chdir("golangCompiler/src")

	doCommand("git", []string{"checkout", "go1.7.5"})
	doCommand("bash", []string{"all.bash"})
	os.Chdir(cwd)
	os.Setenv("GOROOT", fmt.Sprintf("%v/golangCompiler/", cwd))
	os.Setenv("PATH", fmt.Sprintf("%v/golangCompiler/bin/:%v", cwd, os.Getenv("PATH")))
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

	renderStr, _ := ascii.RenderOpts(s, options)
	return renderStr
}

func buildGcc(path string) {
	arch := "x86_64"
	targetDir := "fakeRoot"
	os.Chdir(path)
	fmt.Println(figlet("GMP"))
	//doCommand("git", []string{"clone", "https://github.com/bw-oss/gmp"})
	doCommand("tar", []string{"-xjvf", "zips/gmp-4.3.2.tar.bz2"})
	os.Chdir("gmp-4.3.2")
	//We need build= because the buck-toothed, cow-humping retards who use autoconf can't figure out I have the most common CPU architecture in the world
	//So glad you wrote that stupid little script to help!
	doCommand("./configure", []string{"--disable-shared", "--enable-static", fmt.Sprintf("--prefix=%v/%v", path, targetDir), fmt.Sprintf("--build=%v", arch)})
	doCommand("make", []string{})
	doCommand("make", []string{"install"})

	//gcc-6.3.0.tar.gz  gmp-4.3.2.tar.bz2  isl-0.15.tar.bz2  mpc-0.8.1.tar.gz  mpfr-2.4.2.tar.bz2

	os.Chdir(path)

	fmt.Println(figlet("MPFR"))
	doCommand("tar", []string{"-xjvf", "zips/mpfr-2.4.2.tar.bz2"})

	os.Chdir("mpfr-2.4.2")
	doCommand("chmod", []string{"a+rwx", "configure"})
	doCommand("./configure", []string{"--disable-shared", "--enable-static", fmt.Sprintf("--with-gmp=%v/%v", path, targetDir), fmt.Sprintf("--prefix=%v/%v", path, targetDir)})
	doCommand("make", []string{})
	doCommand("make", []string{"install"})

	os.Chdir(path)

	fmt.Println(figlet("MPC"))
	doCommand("tar", []string{"-xzvf", "zips/mpc-0.8.1.tar.gz"})
	os.Chdir("mpc-0.8.1")
	doCommand("chmod", []string{"a+rwx", "configure"})
	doCommand("./configure", []string{"--disable-shared", "--enable-static", fmt.Sprintf("--with-gmp=%v/%v", path, targetDir), fmt.Sprintf("--with-mpfr=%v/%v", path, targetDir), fmt.Sprintf("--prefix=%v/%v", path, targetDir)})
	doCommand("make", []string{})
	doCommand("make", []string{"install"})

	os.Chdir(path)

	fmt.Println(figlet("ISL"))
	doCommand("tar", []string{"-xjvf", "zips/isl-0.15.tar.bz2"})
	os.Chdir("isl-0.15")
	doCommand("chmod", []string{"a+rwx", "configure"})
	doCommand("./configure", []string{"--disable-shared", "--enable-static", fmt.Sprintf("--with-gmp=%v/%v", path, targetDir), fmt.Sprintf("--with-mpfr=%v/%v", path, targetDir), fmt.Sprintf("--with-mpc=%v/%v", path, targetDir), fmt.Sprintf("--with-elf=%v/%v", path, targetDir), fmt.Sprintf("--prefix=%v/%v", path, targetDir)})
	doCommand("make", []string{})
	doCommand("make", []string{"install"})

	os.Chdir(path)
	fmt.Println(figlet("GCC"))
	os.Chdir("gcc/objdir")
	doCommand(fmt.Sprintf("/%v/gcc/configure", path), []string{"--enable-languages=c,c++,go", "--disable-shared", "--enable-static", "--disable-multilib", "--disable-shared", "--enable-static", fmt.Sprintf("--with-gmp=%v/%v", path, targetDir), fmt.Sprintf("--with-mpfr=%v/%v", path, targetDir), fmt.Sprintf("--with-mpc=%v/%v", path, targetDir), fmt.Sprintf("--with-isl=%v/%v", path, targetDir), fmt.Sprintf("--prefix=%v/%v", path, targetDir)})
	doCommand("make", []string{})
	doCommand("make", []string{"install"})

	os.Chdir(path)
}

func main() {
	printEnv()
	folderPath, err := osext.ExecutableFolder()
	myDir := fmt.Sprintf("%v/goFiles", folderPath)
	fmt.Println("I> Creating", myDir)
	os.Mkdir(myDir, os.ModeDir|0777)
	if err != nil {
		os.Exit(1)
	}
	os.Setenv("GOPATH", myDir)
	os.Setenv("GOROOT_BOOTSTRAP", runtime.GOROOT())
	printEnv()
	unPackGoMacOSX(folderPath)

	//os.Exit(0)

	buildGcc(folderPath)
	fmt.Println(figlet("GO COMPILER"))
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
