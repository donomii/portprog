# Make your own package

Making your own packages is simple and easy.

1. Open the packages-devel directory

2. Create a JSON file with the details of your program

3. In a terminal, run ```portprog --file packages-windows\yourfile.json```

4. Run ```environment.bat``` and then test your new package

## The JSON file

What do you put in your JSON file?  Let's take a look at cmder.json

```json
{
    	"Name"  :   "cmder",
    	"Zip"   :   "cmder.7z",
    	"Url"   :   "https://github.com/cmderdev/cmder/releases/download/v1.3.11/cmder.7z",
	"Fetch" :   "web",
	"Plan"  :   "zipWithNoDirectory",
	"Type"  :   "Application",
	"BinDir":   "/"
}
```

The JSON file contains all the information needed to download and install your program.  Let's take a look at the fields:

* Url - Download the zip from here
* Zip - The name of the zip file that will appear on disk.  Portprog uses wget to download the zips.
* Name - is the name of the target directory in the install directory.  See the options ```zipWithDirectory``` and ```zipWithNoDirectory``` for more information
* Fetch - only web is supported for now
* Plan - Currently has three options, depending on the zip type.
	1. ```zipWithDirectory``` - Your program is in a zip file (.zip, .tar, .7z, .rar).  The zip file contains a directory, and all your programs are inside that.  In this case, you MUST use the name of that directory in the ```Name``` field.
	2. ```zipWithNoDirectory``` - Your program is in a zip file, but all the files are at the top level.  Portprog will create the directory named in the ```Name``` field, and unpack your directory there.
	3. ```msi``` - You are using an MSI installer package.  The package will be unpacked into the directory named in the ```Name``` field, and your program probably won't work
	4. There is no 4
* Type - Only application, for now.  If your zip file doesn't contain an application (e.g. it is just data), leave the field empty
* BinDir - The directory, _inside the zip file_, that contains the executables for your program.  Usually "/bin" or just "/".  If "", this directory will not be added to the path variable.

Good luck and enjoy!
