.PHONY: build fmt tidy vet

build:
	go build github.com/heartleo/zlib/cmd/zlib

fmt:
	go fmt ./...

tidy:
	go mod tidy

vet:
	go vet ./...
