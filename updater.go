package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kardianos/osext"
	"github.com/probandula/figlet4go"
)

var wg sync.WaitGroup
var installDir = "installs"
var goExe = "installs/go/bin/go"
var cpanExe = "installs/strawberry-perl/perl/bin/cpan"
var gitExe = "installs/PortableGit-2.15.0"
var tempDir = "temp"
var noGcc = false
var noGo = false
var noGit = false
var noInstall = false
var develMode = false

var subPaths []string = []string{
	"installs/PortableGit-2.15.0/bin",
	"installs/PortableGit-2.15.0/usr/bin",
	"installs/go/bin",
	"7zip",
	"installs/components-15.3.7/bin",
	"installs/strawberry/perl/site/bin",
	"installs/strawberry-perl/perl/bin",
	"installs/strawberry-perl/c/bin",
	"langlibs/gopath/bin",
}

func MoveFile(sourcePath, destPath string) error {
	inputFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("Couldn't open source file: %s", err)
	}
	outputFile, err := os.Create(destPath)
	if err != nil {
		inputFile.Close()
		return fmt.Errorf("Couldn't open dest file: %s", err)
	}
	defer outputFile.Close()
	_, err = io.Copy(outputFile, inputFile)
	inputFile.Close()
	if err != nil {
		return fmt.Errorf("Writing to output file failed: %s", err)
	}
	// The copy was successful, so now delete the original file
	err = os.Remove(sourcePath)
	if err != nil {
		return fmt.Errorf("Failed removing original file: %s", err)
	}
	return nil
}

func downloadFile(tempPath, finalfilepath string, url string) (err error) {
	fmt.Printf("I> Downloading %v to %v\n", url, tempPath)
	if _, err := os.Stat(finalfilepath); os.IsNotExist(err) {
		// Get the data
		resp, err := http.Get(url)
		if err != nil {
			fmt.Printf("E> %v\n", err)
			return err
		}
		defer resp.Body.Close()

		// Create the file
		out, err := os.Create(tempPath)
		if err != nil {
			fmt.Printf("E> %v\n", err)
			return err
		}

		// Writer the body to file
		_, err = io.Copy(out, resp.Body)
		if err != nil {
			fmt.Printf("E> %v\n", err)
			return err
		}
		defer out.Close()
		MoveFile(tempPath, finalfilepath)
	}
	return nil
}

func doCommand(cmd string, args []string) string {
	if noInstall {
		return ""
	}
	fmt.Println("C>", cmd, args)
	out, err := exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "IO> %v\n", string(out))
		fmt.Fprintf(os.Stderr, "E> %v\n", err)
		//os.Exit(1)
	}
	if string(out) != "" {
		fmt.Fprintf(os.Stderr, "O> %v\n\n", string(out))
	}
	return string(out)
}

func copyFile(source, target string) {
	doCommand("/bin/cp", []string{"-r", source, target})
}

//Takes a path to a dmg file, mounts it, then return the mount point and device path
func attachDMG(path string) (string, string) {
	results := doCommand("/usr/bin/hdiutil", []string{"attach", path})
	r := regexp.MustCompile(`(/dev/\S+)\s+Apple_HFS\s+(.*)`)
	b := r.FindStringSubmatch(results)
	s := regexp.MustCompile(`\s+`)
	bits := s.Split(b[0], 3)
	//fmt.Println(bits)
	device := string(bits[0])
	mountpoint := string(bits[2])
	fmt.Printf("\n!!!device: %v, mountpoint: %v\n", device, mountpoint)
	return mountpoint, device
}

func detachDMG(device string) {
	doCommand("/usr/bin/hdiutil", []string{"detach", device})
}

func buildGithub(repo string) {
	cmd := goExe
	args := []string{"build", repo}
	fmt.Printf("I> Building %v\n", repo)
	doCommand(cmd, args)
}

func installGoGithub(repo string) {
	cmd := goExe
	args := []string{"get", "-u", repo}
	fmt.Printf("I> Installing %v\n", repo)
	doCommand(cmd, args)
}

func installGithub(repo string) {

	cmd := gitExe
	args := []string{"clone", repo}
	fmt.Printf("I> Cloning %v\n", repo)
	doCommand(cmd, args)
}

func installCpan(repo string) {
	cmd := force_winpath(cpanExe)
	args := []string{repo}
	fmt.Printf("I> Installing %v\n", repo)
	doCommand(cmd, args)
}

func loadRepos(filename string) []string {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		//Do something
	}
	lines1 := strings.Split(string(content), "\r\n")
	lines2 := strings.Split(string(content), "\n")
	if len(lines1) > len(lines2) {
		return lines1
	} else {
		return lines2
	}
}

func unPackGoMacOSX(b Config, folderPath string) {
	//doCommand("xar", []string{"-xf", "go1.7.5.darwin-amd64.tar.gz"})
	//doCommand("sh", []string{"-c", "cat com.googlecode.go.pkg/Payload | gunzip -dc | cpio -i"})
	unTgzLib(b, "go1.7.5.darwin-amd64")
	os.Setenv("GOROOT", fmt.Sprintf("%v/go/", folderPath))
	os.Setenv("PATH", fmt.Sprintf("%v/go/bin/:%v", folderPath, os.Getenv("PATH")))
	doCommand("go", []string{"version"})
}

func buildGo(goDir string) {
	figlet("COMPILING GO")
	cwd, _ := os.Getwd()
	fmt.Println(fmt.Sprintf("I> Deleting directory %v", goDir))
	//doCommand("rm", []string{"-r", goDir})
	doCommand("git", []string{"clone", "https://go.googlesource.com/go", goDir})
	os.Chdir(fmt.Sprintf("%v/src", goDir))

	doCommand("git", []string{"checkout", "go1.7.5"})

	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		doCommand("bash", []string{"all.bash"})
		os.Chdir(cwd)
		os.Setenv("GOROOT", fmt.Sprintf("%v/%v/", goDir, cwd))
		os.Setenv("PATH", fmt.Sprintf("%v/%v/bin/:%v", cwd, goDir, os.Getenv("PATH")))
	} else {
		doCommand("all.bat", []string{})
		os.Chdir(cwd)
		os.Setenv("GOROOT", fmt.Sprintf("%v\\%v\\", goDir, cwd))
		os.Setenv("PATH", fmt.Sprintf("%v\\%v\\bin\\:%v", cwd, goDir, os.Getenv("PATH")))
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

func unTar(b Config, zipPath string) {
	p2 := strings.Replace(zipPath, ".gz", "", -1)
	p2 = strings.Replace(zipPath, ".tgz", ".tar", -1)
	p2 = strings.Replace(p2, ".lzma", "", -1)
	p2 = strings.Replace(p2, ".bz2", "", -1)
	splits := strings.Split(p2, "/")
	fname := splits[len(splits)-1]
	if _, err := os.Stat(zipPath); err == nil {
		if isWindows() {
			unSevenZ(b, zipPath)
		} else {
			doCommand("tar", []string{"-xzvf", zipPath})
			doCommand("tar", []string{"-xjvf", zipPath})
		}
	} else {
		log.Println("Could not find ", fname)
	}
}

func unTgzLib(b Config, zipPath string) {
	p2 := strings.Replace(zipPath, ".gz", "", -1)
	splits := strings.Split(p2, "/")
	fname := splits[len(splits)-1]
	if _, err := os.Stat(zipPath); err == nil {
		if isWindows() {
			unSevenZ(b, zipPath)

			//unSevenZ(b, fname)
		} else {
			doCommand("tar", []string{"-xzvf", zipPath})
		}
	} else {
		log.Println("Could not extract ", fname)
	}
}

func unBzLib(b Config, lib string) {
	path := fmt.Sprintf("%v/%v.tar.bz2", b.ZipDir, lib)
	if _, err := os.Stat(path); err == nil {
		if isWindows() {

			unSevenZ(b, path)
		} else {
			doCommand("tar", []string{"-xjvf", path})
		}
	}
}

func recurseRemove(path string) {
	os.RemoveAll(path)
}

func chmod(perms, path string) {
	if !isWindows() {
		doCommand("chmod", []string{perms, path})
	}
}

func standardConfigureBuild(b Config, name, buildDir string, args []string) {
	cwd, _ := os.Getwd()
	configurePath := fmt.Sprintf("%v/%v/%v", cwd, name, "configure")
	unBzLib(b, name)
	unTgzLib(b, name)
	os.Chdir(name)
	os.Chdir(buildDir)
	chmod("a+rwx", "configure")
	doCommand(configurePath, args)
	doCommand("make", []string{})
	doCommand("make", []string{"install"})
	os.Chdir(cwd)
	recurseRemove(name)
}

func buildGcc(b Config, path string) {
	arch := "x86_64"
	targetDir := fmt.Sprintf("%v/%v", path, installDir)
	//srcDir := fmt.Sprintf("%v/src", path)
	os.Chdir(path)
	fmt.Println(figlet("GMP"))
	//doCommand("git", []string{"clone", "https://github.com/bw-oss/gmp"})
	gmpName := "gmp-6.1.2"
	mpfrName := "mpfr-3.1.5"
	mpcName := "mpc-1.0.3"
	gccName := "gcc-6.3.0"
	islName := "isl-0.15"

	standardConfigureBuild(b, gmpName, ".", []string{"--disable-shared", "--enable-static", makeOpt("prefix", targetDir), makeOpt("build", arch)})
	standardConfigureBuild(b, mpfrName, ".", []string{"--disable-shared", "--enable-static", makeWith("gmp", targetDir, ""), makeOpt("prefix", targetDir)})
	standardConfigureBuild(b, mpcName, ".", []string{"--disable-shared", "--enable-static", makeOpt("with-gmp", targetDir), makeOpt("with-mpfr", targetDir), makeOpt("prefix", targetDir)})
	standardConfigureBuild(b, islName, ".", []string{"--disable-shared", "--enable-static", makeOpt("with-gmp-prefix", targetDir), makeOpt("prefix", targetDir)})
	standardConfigureBuild(b, gccName, "gcc/objdir", []string{"--enable-languages=c,c++,go", "--disable-shared", "--enable-static", "--disable-multilib", "--disable-shared", "--enable-static", makeWith("gmp", targetDir, ""), makeWith("mpfr", targetDir, ""), makeWith("mpc", targetDir, ""), makeWith("isl", targetDir, ""), makeOpt("prefix", targetDir)})
}

func unSevenZ(b Config, file string) {
	fmt.Println(b.SzPath, file)
	wg.Add(1)

	startpipe := make(chan int)
	if false {
		func() {
			args := []string{b.SzPath, "x", file, "-aoa"}
			log.Printf("Args: %v\n", args)
			os.StartProcess(b.SzPath, args, &os.ProcAttr{})
			startpipe <- 1
			//FIXME start another thread to monitor the unzip and call wg.Done when it finishes
		}()
		<-startpipe
	}
	if !noInstall {

		func() {
			defer wg.Done()
			//startpipe <- 1 //Go functions can take a few seconds to be scheduled
			doCommand(b.SzPath, []string{"x", file, "-aoa"})

		}()
		//<-startpipe
		time.Sleep(1 * time.Second)

	}
	if false {
		defer wg.Done()
		doCommand(b.SzPath, []string{"x", file, "-aoa"})

	}
}

/*
func unzipWithPathMake(zipName) {
	cwd, _ := os.Getwd()
	os.Chdir(srcDir)
	unSevenZ(b, "../zips/zeromq-4.2.1.zip")
	os.Chdir("zeromq-4.2.1/zeromq-4.2.1/builds/mingw32")
	doCommand("make", []string{})
	os.Chdir(zipName)
}
*/

func Make(b Config, p Package) {
	cwd, _ := os.Getwd()
	os.Chdir(b.InstallDir)
	os.Chdir(p.Name)
	here, _ := os.Getwd()
	fmt.Printf("Making in %v\n", here)
	doCommand("make", []string{"install"})
	os.Chdir(cwd)
}

func goGetAndMake(targetDir, name, goPath, url, p1 string) {
	if noGit {
		return
	}
	//p1 is the branch name
	cwd, _ := os.Getwd()
	doCommand("go", []string{"get", "-u", url})
	os.Chdir(goPath)
	os.Chdir("src")
	os.Chdir(url)
	doCommand("git", []string{"checkout", p1})
	thsDir, _ := os.Getwd()
	fmt.Printf("Making in %v\n", thsDir)
	doCommand("make", []string{"install"})
	os.Chdir(cwd)
}

func zipFilePath(b Config, name string) string {
	path := fmt.Sprintf("%v/%v", b.ZipDir, name)
	return path
}

func zipWithDirectory(b Config, p Package) {
	cwd, _ := os.Getwd()
	targetDir := fmt.Sprintf("%v", b.InstallDir) //Make an appsdir and install there?
	os.Mkdir(targetDir, os.ModeDir|0777)
	os.Chdir(targetDir)

	unTar(b, zipFilePath(b, p.Zip))
	unSevenZ(b, zipFilePath(b, p.Zip))
	os.Chdir(cwd)
}

func zipWithNoDirectory(b Config, p Package) {
	cwd, _ := os.Getwd()
	targetDir := fmt.Sprintf("%v/%v", b.InstallDir, p.Name)
	os.Mkdir(targetDir, os.ModeDir|0777)
	os.Chdir(targetDir)

	unTar(b, zipFilePath(b, p.Zip))
	unSevenZ(b, zipFilePath(b, p.Zip))
	os.Chdir(cwd)
}

func doFetch(p Package, b Config) {
	fetch := p.Fetch
	if fetch == "web" {

		downloadFile(b.TempDir+"/"+p.Zip, fmt.Sprintf("%v/%v", b.ZipDir, p.Zip), p.Url)
	}
	if fetch == "git" {
		if noGit {
			return
		}
		url := p.Url
		branch := p.Branch
		name := p.Name
		targetDir := b.InstallDir
		cwd, _ := os.Getwd()
		os.Chdir(targetDir)
		doCommand("git", []string{"clone", url, name, "--recursive"})
		os.Chdir(name)
		doCommand("git", []string{"checkout", branch})
		doCommand("git", []string{"submodule", "foreach", "--recursive", "git", "checkout", "master"})
		os.Chdir(cwd)
	}
}

func doGit(p Package, b Config) {
	url := p.Url
	branch := p.Branch
	name := p.Name
	targetDir := b.InstallDir
	cwd, _ := os.Getwd()
	os.Chdir(targetDir)
	doCommand("git", []string{"clone", url, name})
	os.Chdir(name)
	doCommand("git", []string{"checkout", branch})
	os.Chdir(cwd)
}

func force_winpath(str string) string {
	winpath := strings.Replace(str, "/", "\\", -1)
	return winpath
}

func msi(b Config, p Package) {
	targetDir := fmt.Sprintf("%v/%v", b.InstallDir, p.Name)
	fmt.Println(b.SzPath, zipFilePath(b, p.Zip))
	// we can use /i to do full installs including registry updates, but that requires the user to click things
	// prefer /a because it just dumps the files in the directory, and most open source programs don't actually
	// use the registry
	args := []string{"/a", force_winpath(zipFilePath(b, p.Zip)), fmt.Sprintf("INSTALLLOCATION=%v", force_winpath(targetDir)),
		fmt.Sprintf("INSTALLDIR=%v", force_winpath(targetDir)),
		fmt.Sprintf("TARGETDIR=%v", force_winpath(targetDir)),
		"/q", "/lv", "log.txt"}
	doCommand("msiexec.exe", args)
}

func dmg(b Config, p Package) {
	targetDir := fmt.Sprintf("%v/%v", b.InstallDir, p.Name)
	fmt.Println("I> installing ", zipFilePath(b, p.Zip), " to ", targetDir)
	mountpoint, device := attachDMG(zipFilePath(b, p.Zip))
	fmt.Println("I> Mounted DMG on ", mountpoint)
	copyFile(mountpoint+"/"+p.Name+".app", b.InstallDir)
	detachDMG(device)
}

func doAll(p Package, b Config) {
	figSay(p.Name)
	targetDir := b.InstallDir
	doFetch(p, b)

	cwd, _ := os.Getwd()
	os.Chdir(targetDir)

	//unBzLib(b, p.Name)
	//unTgzLib(b, p.Name)

	plan := p.Plan
	os.Chdir(cwd)
	if plan == "standardConfigure" {
		standardConfigureBuild(b, p.Name, ".", []string{makeOpt("prefix", targetDir)}) //, makeOpt("with-sysroot", targetDir)
	} else if plan == "goGetAndMake" {
		goGetAndMake(targetDir, p.Name, b.InstallDir, p.Url, p.Branch) //use zip field as goPath
	} else if plan == "gitAndMake" {
		Make(b, p) //use zip field as srcDir (i.e. buildDir)
	} else if plan == "zipWithNoDirectory" {
		zipWithNoDirectory(b, p)
	} else if plan == "zipWithDirectory" {
		zipWithDirectory(b, p)
	} else if plan == "msi" {
		msi(b, p)
	} else if plan == "dmg" {
		dmg(b, p)
	} else if plan == "customCommand" {
		//customCommand(b, p)
	} else {
		log.Println("Unknown plan: ", plan)
	}

	if p.BinDir != "" {
		subPaths = append(subPaths, b.InstallDir+"/"+p.Name+"/"+p.BinDir)
	}
	for _, v := range p.BinDirs {
		subPaths = append(subPaths, b.InstallDir+"/"+p.Name+"/"+v)
	}
	for _, v := range p.Deletes {
		os.Remove(b.InstallDir + "/" + p.Name + "/" + v)
	}
}

func figSay(s string) {
	fmt.Println(figlet(s))
}

var working = 0

func processDir(b Config, d string) {
	files, err := ioutil.ReadDir(d)
	if err != nil {
		log.Fatal(err)
	}

	for _, file := range files {
		//working = working + 1

		fname := fmt.Sprintf("%v/%v", d, file.Name())

		if _, err := os.Stat(fname); os.IsNotExist(err) {
			fname = fmt.Sprintf("%v\\%v", d, file.Name())
		}
		p := LoadJSON(fname)

		doAll(p, b)
		//working--

	}

}

func isWindows() bool {
	return runtime.GOOS == "windows"
}

func isOSX() bool {
	return runtime.GOOS == "darwin"
}

func main() {
	flag.BoolVar(&noGcc, "no-gcc", false, "Don't install gcc locally")
	flag.BoolVar(&noGo, "no-golang", false, "Don't install the Go compiler")
	flag.BoolVar(&noGit, "no-git", false, "Don't attempt to clone or update with git")
	flag.BoolVar(&noInstall, "no-install", false, "Don't install anything")
	flag.BoolVar(&develMode, "devel", false, "Only process packages-develop directory")

	flag.Parse()
	printEnv()
	figSay(runtime.GOOS)
	os.Setenv("CFLAGS", "-D_XOPEN_SOURCE=1")
	folderPath, err := osext.ExecutableFolder()
	if err != nil {
		os.Exit(1)
	}

	//Update the predefined paths from relative to absolute
	for i, v := range subPaths {
		subPaths[i] = fmt.Sprintf("%v/%v", folderPath, v)
	}

	langlibs := fmt.Sprintf("%v/langlibs", folderPath)
	gopathDir := fmt.Sprintf("%v/gopath", langlibs)
	cpanDir := fmt.Sprintf("%v/cpan", langlibs)
	zipsDir := fmt.Sprintf("%v/zips", folderPath)
	rootDir := fmt.Sprintf("%v/%v", folderPath, installDir)
	srcDir := fmt.Sprintf("%v/src", folderPath)
	SzDir := fmt.Sprintf("%v/7zip", folderPath)
	SzPath := fmt.Sprintf("%v/7zip/7z.exe", folderPath)
	goDir := fmt.Sprintf("%v/go", folderPath)
	tmpDir := fmt.Sprintf("%v/%v", folderPath, tempDir)
	fmt.Println("I> Creating directories")
	os.Mkdir(tmpDir, os.ModeDir|0777)
	os.Mkdir(langlibs, os.ModeDir|0777)
	os.Mkdir(gopathDir, os.ModeDir|0777)
	os.Mkdir(cpanDir, os.ModeDir|0777)
	os.Mkdir(zipsDir, os.ModeDir|0777)
	os.Mkdir(rootDir, os.ModeDir|0777)
	os.Mkdir(SzDir, os.ModeDir|0777)
	os.Mkdir(srcDir, os.ModeDir|0777)
	os.Mkdir("packages-develop", os.ModeDir|0777)
	fmt.Println("I> Creating ", goDir)
	os.Mkdir(goDir, os.ModeDir|0777)
	os.Setenv("GOPATH", gopathDir)
	os.Setenv("GOROOT_BOOTSTRAP", runtime.GOROOT())

	os.Chdir(folderPath)

	var b Config
	b.InstallDir = rootDir
	b.GoPath = gopathDir
	b.SourceDir = srcDir
	b.SzPath = SzPath
	b.ZipDir = zipsDir
	b.TempDir = tmpDir

	fmt.Printf("I> Selected configuration: %+v\n", b)
	//b.SiloDir = fmt.Sprintf("%v/silo", folderPath)
	//os.Mkdir(b.SiloDir, os.ModeDir|0777)

	downloadFile(b.TempDir+"/7z1604.exe", "zips/7z1604.exe", b.ZipDir+"http://www.7-zip.org/a/7z1604.exe")

	if isWindows() {
		figSay("7zip")
		doCommand("zips/7z1604.exe", []string{"/S", fmt.Sprintf("/D=%v", SzDir)})
	}

	//fetchBuild(rootDir, "libelf-0.8.13", "libelf-0.8.13.tar.gz", "http://www.mr511.de/software/libelf-0.8.13.tar.gz", "standardConfigure", "")
	//fetchBuild(rootDir, "busybox-w32", srcDir, "https://github.com/rmyorston/busybox-w32", "gitAndMake", "master")
	//fetchBuild(rootDir, "busybox", srcDir, "git://busybox.net/busybox.git", "gitAndMake", "trunk")

	downloadFile(b.TempDir+"/nuwen-15.3.7.7z", "zips/nuwen-15.3.7.7z", "https://nuwen.net/files/mingw/components-15.3.7z")
	downloadFile(b.TempDir+"/Sources.gz", "zips/Sources.gz", "http://nl.archive.ubuntu.com/ubuntu/dists/devel/main/source/Sources.gz")
	downloadFile(b.TempDir+"/gcc-5.1.0-tdm64-1-core.zip", "zips/gcc-5.1.0-tdm64-1-core.zip", "https://kent.dl.sourceforge.net/project/tdm-gcc/TDM-GCC%205%20series/5.1.0-tdm64-1/gcc-5.1.0-tdm64-1-core.zip")

	downloadFile(b.TempDir+"/gmp-6.1.2.tar.bz2", "zips/gmp-6.1.2.tar.bz2", "https://gmplib.org/download/gmp/gmp-6.1.2.tar.bz2")
	figSay("GCC COMPILER")
	//os.Exit(0)
	if !noGcc {
		if !isWindows() {
			buildGcc(b, folderPath)
		} else {
			os.Chdir(rootDir)

			doCommand("../7zip/7z.exe", []string{"x", "../zips/nuwen-15.3.7.7z"})
			os.Chdir("components-15.3")
			files, err := ioutil.ReadDir(".")
			if err != nil {
				log.Fatal(err)
			}

			for _, file := range files {
				if strings.HasSuffix(file.Name(), "7z") {

					unSevenZ(b, file.Name())
				}
			}
			os.Chdir(folderPath)
			printEnv()

		}
	}
	os.Setenv("PATH", fmt.Sprintf("%v/components-15.3/bin/;%v", rootDir, os.Getenv("PATH")))

	if !noGo {
		fmt.Println(figlet("GO COMPILER"))
		os.Mkdir(goDir, os.ModeDir|0777)

		printEnv()
		if runtime.GOOS == "darwin" {
			figSay("Unpacking Golang")
			unPackGoMacOSX(b, folderPath)
		} else if runtime.GOOS == "windows" {
			os.Chdir(goDir)
			figSay("Unpacking Golang")
			unSevenZ(b, "../zips/go1.7.5.windows-amd64.zip")
			os.Chdir(folderPath)
		} else {
			os.Setenv("GOROOT", goDir)
			figSay("Building Golang")
			buildGo(goDir)
		}
		printEnv()
	}
	if develMode {
		processDir(b, "packages-develop")
	} else {
		if isWindows() {
			processDir(b, "packages-windows")
		} else {
			processDir(b, "packages")
			if isOSX() {
				processDir(b, "packages-osx")
			}
		}
	}

	var repos []string
	if !develMode {
		figSay("CPAN")
		working++
		func() {
			repos = loadRepos("packages-other/cpan")
			for _, v := range repos {
				v = strings.Replace(v, "\r", "", -1)
				installCpan(v)
			}
			working--
		}()
	}

	if !develMode {
		if !noGit {
			figSay("LIBRARIES")
			repos = loadRepos("packages-other/go_libs")
			for _, v := range repos {
				v = strings.Replace(v, "\r", "", -1)
				installGoGithub(v)
			}

			figSay("APPLICATIONS")
			repos = loadRepos("packages-other/go_apps")
			for _, v := range repos {
				v = strings.Replace(v, "\r", "", -1)
				installGoGithub(v)
			}

			figSay("GITHUB")
			repos = loadRepos("packages-other/github")
			os.Mkdir("git", 0777)
			os.Chdir(fmt.Sprintf("%v/git", folderPath))

			for _, v := range repos {
				v = strings.Replace(v, "\r", "", -1)
				installGithub(v)
			}
			os.Chdir(folderPath)
		}
	}

	fmt.Println(figlet("ENVIRONMENT"))
	fmt.Printf("\nNow set your path with one of the following commands\n\n")

	fmt.Println("Windows:")
	var text string = fmt.Sprintf("set GOPATH=%v/go\n", folderPath)
	for _, v := range subPaths {
		winpath := strings.Replace(v, "/", "\\", -1)
		text = fmt.Sprintf("%sset PATH=%v;%%PATH%%\n", text, winpath)
	}
	text = text + "\nstart cmd /k cmder\nstart cmd /k cmd"
	fmt.Println(text)
	ioutil.WriteFile("environment.bat", []byte(text), 0644)
	text = ""

	fmt.Println("Fish Shell:")
	for _, v := range subPaths {
		text = fmt.Sprintf("%sset -x %v/%v $PATH\n", text, folderPath, v)
	}
	fmt.Println(text)
	ioutil.WriteFile("environment.fish", []byte(text), 0644)
	text = ""

	fmt.Println("Bash Shell")
	for _, v := range subPaths {
		text = fmt.Sprintf("%sexport PATH=%v:$PATH\n", text, v)
	}
	fmt.Println(text)
	ioutil.WriteFile("environment.bash", []byte(text), 0644)
	text = ""

	for {
		time.Sleep(1 * time.Second)
	}

	fmt.Printf("Job's a good'un, boss\n")
}

func setCommand(p string) string {
	return fmt.Sprintf("set -x PATH %v $PATH\nexport PATH=%v/:$PATH\nset PATH=%v;\n\n\n", p, p, p)
}
