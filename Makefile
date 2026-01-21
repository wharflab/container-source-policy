.PHONY: build test lint lint-fix clean

build:
	go build -ldflags "-s -w" -o container-source-policy

test:
	go test -race -count=1 -timeout=30s ./...

GOLANGCI_LINT_VERSION := v2.8.0

lint: bin/golangci-lint-$(GOLANGCI_LINT_VERSION)
	bin/golangci-lint run

lint-fix: bin/golangci-lint-$(GOLANGCI_LINT_VERSION)
	bin/golangci-lint run --fix

bin/golangci-lint-$(GOLANGCI_LINT_VERSION):
	@rm -f bin/golangci-lint bin/golangci-lint-*
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh | sh -s -- -b bin/ $(GOLANGCI_LINT_VERSION)
	@touch $@

clean:
	rm -f container-source-policy
	rm -rf bin/ dist/
