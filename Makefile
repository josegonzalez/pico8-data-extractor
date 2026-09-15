NAME = pico8-data-extractor
DIST ?= dist
VERSION ?= dev
LDFLAGS = -s -w -X main.Version=$(VERSION)

# sha256sum on Linux, shasum on macOS. Expanded once with :=, since command -v
# has no reason to run again for every rule that needs it.
SHASUM := $(shell command -v sha256sum >/dev/null && echo sha256sum || echo "shasum -a 256")

# Every release target. GOARM only matters for linux/arm, where 7 covers the
# ARMv7 handhelds this tool is built for.
RELEASE_BINARIES = \
	$(DIST)/$(NAME)-darwin-amd64 \
	$(DIST)/$(NAME)-darwin-arm64 \
	$(DIST)/$(NAME)-linux-amd64 \
	$(DIST)/$(NAME)-linux-arm \
	$(DIST)/$(NAME)-linux-arm64 \
	$(DIST)/$(NAME)-windows-amd64.exe

.PHONY: build build-release clean lint test

build:
	go build -ldflags "$(LDFLAGS)" -o $(NAME) .

build-release: $(DIST)/checksums.txt

$(DIST)/$(NAME)-darwin-amd64:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(NAME)-darwin-amd64 .

$(DIST)/$(NAME)-darwin-arm64:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(NAME)-darwin-arm64 .

$(DIST)/$(NAME)-linux-amd64:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(NAME)-linux-amd64 .

$(DIST)/$(NAME)-linux-arm:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(NAME)-linux-arm .

$(DIST)/$(NAME)-linux-arm64:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(NAME)-linux-arm64 .

$(DIST)/$(NAME)-windows-amd64.exe:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(NAME)-windows-amd64.exe .

# The $(NAME)-* glob keeps checksums.txt from hashing itself.
$(DIST)/checksums.txt: $(RELEASE_BINARIES)
	cd $(DIST) && $(SHASUM) $(NAME)-* > checksums.txt

clean:
	rm -f $(NAME)
	rm -rf dist

lint:
	golangci-lint run --config=./.golangci.yml

test:
	go test ./...
