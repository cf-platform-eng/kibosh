default: all

GO_PACKAGES = $$(go list ./... ./cmd/loader | grep -v vendor | grep -v tools)
GO_FILES = $$(find . -name "*.go" | grep -v vendor | uniq)

build-kibosh-linux:
	GOOS=linux GOARCH=amd64 go build -o kibosh.linux ./cmd/kibosh/main.go

build-kibosh-mac:
	GOOS=darwin GOARCH=amd64 go build -o kibosh.darwin ./cmd/kibosh/main.go

build-kibosh: build-kibosh-linux build-kibosh-mac

build-loader-linux:
	GOOS=linux GOARCH=amd64 go build -o loader.linux ./cmd/loader/main.go

build-loader-mac:
	GOOS=darwin GOARCH=amd64 go build -o loader.mac ./cmd/loader/main.go

build-loader: build-loader-linux build-loader-mac

build-bazaar-mac:
	GOOS=darwin GOARCH=amd64 go build -o bazaar.mac ./cmd/bazaarapi/main.go

build-bazaar-linux:
	GOOS=linux GOARCH=amd64 go build -o bazaar.linux ./cmd/bazaarapi/main.go

build-bazaar: build-bazaar-linux build-bazaar-mac

build-bazaar-cli-mac:
	GOOS=darwin GOARCH=amd64 go build -o bazaarcli.mac ./cmd/bazaarcli/main.go

build-bazaar-cli-linux:
	GOOS=linux GOARCH=amd64 go build -o bazaarcli.linux ./cmd/bazaarcli/main.go

build-bazaar-cli: build-bazaar-cli-mac build-bazaar-cli-linux

build-template-tester-mac:
	GOOS=darwin GOARCH=amd64 go build -o template-tester.mac ./cmd/templatetester/main.go

build-template-tester-linux:
	GOOS=linux GOARCH=amd64 go build -o template-tester.linux ./cmd/templatetester/main.go

build-template-tester: build-template-tester-mac build-template-tester-linux

unit-test:
	@go test ${GO_PACKAGES}

fmt:
	goimports -l -w $(GO_FILES)

vet:
	@go vet ${GO_PACKAGES}

test: generate unit-test vet

generate:
	go generate ./...

cleandep:
	go mod tidy

bootstrap:
	go install "github.com/maxbrunsfeld/counterfeiter/v6"
	go install "github.com/onsi/ginkgo"
	go install "github.com/onsi/gomega"
	go install "golang.org/x/tools/cmd/goimports"

boostrap: bootstrap

all: fmt test build-kibosh build-loader build-bazaar build-bazaar-cli build-template-tester
quick: fmt test build-kibosh-mac build-loader-mac build-template-tester-mac
