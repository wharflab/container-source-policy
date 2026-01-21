.PHONY: build test lint clean

build:
	go build -ldflags "-s -w" -o container-source-policy

test:
	go test -race -count=1 -timeout=30s ./...

lint: bin/golangci-lint
	bin/golangci-lint run --fix

bin/golangci-lint:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b bin/

clean:
	rm -f container-source-policy
	rm -rf bin/ dist/
