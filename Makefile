.DEFAULT_GOAL := test

# Clean up
clean:
	@rm -fR ./coverage*
.PHONY: clean

# Run tests and generates html coverage file
cover: test
	@go tool cover -html=./coverage.text -o ./coverage.html
.PHONY: cover

# Run tests
test:
	@go test -v -race -coverprofile=./coverage.text -covermode=atomic $(shell go list ./...)
.PHONY: test
