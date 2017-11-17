# The Portable Programmer

Interpreters and libraries for the programmer on the move

# [Download](https://github.com/donomii/portprog/releases)

[Download](https://github.com/donomii/portprog/releases) and double click to install GCC, Go, Perl, support libraries and much more!

I frequently have to install my programming tools on a fresh computer.  It is always frustrating, because it takes me hours to track down every minor library and
  patch that I need to get something compiled.  So I put together this installer to get my environment set up as quickly as possible.

# Install

## Windows

Download a new release from the [Releases page](https://github.com/donomii/portprog/releases).  Unpack it and double click the exe.

## Linux and Mac

	go get -u github.com/donomii/portprog
	go build
	./portprog

## Options

There aren't a lot.  This isn't another distribution, it's just a fancy downloader and unpacker.  There's no dependency management or build flags 
or whatever.

	--no-gcc	Don't download or install gcc
	--no-golang	Don't download or install golang
	--no-git 	Don't attempt to clone or update any repositories via git
	
## Operation

When started portprog checks the packages (or packages-windows) directory, then attempts to download all the files to the zips directory, then unpack
them.  On future runs, it will check the zips directory and use what it finds there, only downloading if it can't find the file.

## Adding your own

The whole purpose of this is to manage your own downloads.  You can easily add any file you want, just go to the packages directory (or packages-windows), copy
a file there, and change it to download from the url you want.

Then rerun portprog, and it will download and unpack your file.

## Uninstall

Delete the directory.  Portprog does not modify any part of your system outside of its own directory.

** Warning **

I can't control other programs and libraries, so when you use a program or library that portprog downloads for you, this third party might change your system.  
I can't stop that, but I try not to use any programs that would do that.

