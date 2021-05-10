.PHONY: vendor
vendor:
	@echo "updating vendor"
	@go mod vendor
	@echo "done updating vendor"

gen: vendor

STANDARD_BINARIES += ./mec/cmd/mec
