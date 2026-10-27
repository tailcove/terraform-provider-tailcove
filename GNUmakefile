.PHONY: build test vet fmt install

build:
	go build ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

# The provider tests run the Terraform CLI against an in-memory API: have terraform on PATH,
# or set TF_ACC_TERRAFORM_PATH.
test:
	go test ./... -count=1

install:
	go install .
