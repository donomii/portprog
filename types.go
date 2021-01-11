package main

type JsonStringArray []string

type Config struct {
	GoPath                  string          //Path to GO library files
	SourceDir               string          //the source (and build) directory
	InstallDir              string          //The directory we install all files into (e.g. fakeRoot)
	Dependencies            JsonStringArray //Package names (filenames) of all required dependencies
	VersionlessDependencies JsonStringArray //Package names, without version number.  We will pick a package matching that name, possibly at random
	SzPath                  string          //Path to sevenzip
	ZipDir                  string
	SiloDir                 string
	TempDir                 string
}
