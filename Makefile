.PHONY: build clean install install-hooks fmt fmt-check

build:
	go build -o bin/ct ./cmd/ct
	go build -o bin/gt ./cmd/gt

install:
	go install ./cmd/ct
	go install ./cmd/gt

clean:
	rm -rf bin/

fmt:
	gofmt -w internal/ cmd/

fmt-check:
	@unformatted=$$(gofmt -l internal/ cmd/); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt: the following files are not formatted:"; \
		echo "$$unformatted"; \
		echo ""; \
		echo "Run 'make fmt' to fix."; \
		exit 1; \
	fi

install-hooks:
	@ln -sf ../../scripts/pre-commit .git/hooks/pre-commit
	@echo "Installed pre-commit hook -> scripts/pre-commit"
