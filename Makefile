all: build

build:
	./build.sh

test:
	./test.sh

run:
	./run.sh

demo:
	./demo.sh

install:
	./install.sh

.PHONY: all build test run demo install
