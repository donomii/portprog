# The Portable Programmer

Languages and libraries for the programmer on the move

# [Download](https://github.com/donomii/portprog/releases)

[Download](https://github.com/donomii/portprog/releases) and double click to install GCC, Go, Perl, support libraries and much more!

# Background

I frequently have to install my programming tools on a fresh computer.  It is always frustrating, because it takes me hours to track down every minor library and patch that I need to get something compiled.  
  
So I made this installer to get myself set up as quickly and easily as possible.

# Software list

Portprog installs programming languages:

* Gcc
* Golang
* Perl
* Nim
* IO
* Lazarus
* Neko
* Squeak

And editors

* CodeBlocks
* Notepad++

And support utilities

* Make
* Cmake
* Git

And also fetches the sources for

* SDL
* OpenAL
* DCSS

Don't see your favourite thing here?  Send me a pull request!

(Or paste it into a bug report, I don't mind so long as I get it)

# Install

### Windows

Download a new release from the [Releases page](https://github.com/donomii/portprog/releases).  Unpack it and double click the exe.

### Linux and Mac

Download a source release from the [Releases page](https://github.com/donomii/portprog/releases), then follow the [build instructions](https://github.com/donomii/portprog).
	
Or checkout the [latest code](https://github.com/donomii/portprog) from github.
	
### Options

	--no-gcc	Don't download or install gcc
	--no-golang	Don't download or install golang
	--no-git 	Don't attempt to clone or update any repositories via git

## Add your own

Add your own downloads!  

You can easily add any download you want.  Go to the packages directory (or packages-windows), copy
a file there, and change it to download your url.

Rerun portprog, and it will download and unpack your file.

## Uninstall

Delete the directory.  Portprog does not modify any part of your system outside of its own directory.

# Download now!

New releases are available at [https://github.com/donomii/portprog/releases](https://github.com/donomii/portprog/releases)
