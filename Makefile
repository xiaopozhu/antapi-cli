.PHONY: build test fmt-check vet verify release-candidate

build:
	go build -trimpath -o bin/antapi ./cmd/antapi

test:
	go test ./...

fmt-check:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }

vet:
	go vet ./...

verify: fmt-check test vet build

# VERSION、COMMIT、OUTPUT_DIR 由总发布入口显式传入。
release-candidate:
	bash scripts/build-release.sh "$(VERSION)" "$(COMMIT)" "$(OUTPUT_DIR)"
