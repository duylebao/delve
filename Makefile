.DEFAULT_GOAL=test

check-cert:
	@go run scripts/make.go check-cert

build:
	@go run scripts/make.go build

install:
	@go run scripts/make.go install

test:
	@go run scripts/make.go test-full

test-proc-run:
	@go run scripts/make.go test-proc-run -v --backend=$(BACKEND) $(RUN)

test-integration-run:
	@go run scripts/make.go test-integration-run -v --backend=$(BACKEND) $(RUN)

vendor:
	@go run scripts/make.go vendor

.PHONY: vendor test-integration-run test-proc-run test check-cert install build
