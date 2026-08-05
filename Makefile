.PHONY: build install

build:
	go build -o axctl .

install: build
	sudo install axctl /usr/local/bin/axctl
	rm -f axctl
