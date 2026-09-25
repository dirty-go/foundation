GOLANG_IMAGE ?= golang:1.20

.PHONY: fmt deps-up lint lint-fix

fmt:
	@echo "Formatting files..."
	@docker run --rm \
		-v $(CURDIR):/workspace \
		--workdir /workspace \
		--entrypoint gofmt \
		$(GOLANG_IMAGE) -w -l -s \
		.

deps-up:
	@echo "Updating all dependencies..."
	@echo "running on $(CURDIR)"
	@docker run --rm \
		-v $(CURDIR):/workspace \
		--workdir /workspace \
		$(GOLANG_IMAGE) /bin/sh -c "go get -u all && go mod tidy"

lint:
	golangci-lint run ./...

lint-fix:
	golangci-lint run --fix ./...
