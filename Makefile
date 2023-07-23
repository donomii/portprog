all: build

build:
	go get "github.com/probandula/figlet4go"
	go build .
