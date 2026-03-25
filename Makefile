.PHONY: build clean install

build:
	go build -o bin/ct ./cmd/ct
	go build -o bin/gt ./cmd/gt

install:
	go install ./cmd/ct
	go install ./cmd/gt

clean:
	rm -rf bin/
