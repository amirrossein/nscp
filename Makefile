APP      := nscp
PKG      := ./cmd/nscp
BIN      := bin/$(APP)
FYNE     ?= fyne

.PHONY: run build test test-integration lint vet package-macos package-windows package-linux clean

run:
	go run $(PKG)

build:
	go build -o $(BIN) $(PKG)

test:
	go test -race ./...

# Builds a throwaway OpenSSH image and starts two containers (requires Docker).
test-integration:
	cd tests/integration && go test -tags integration -count=1 -v .

vet:
	go vet ./...
	go vet -tags integration ./tests/integration

lint:
	golangci-lint run ./...

# Packaging uses the fyne CLI: go install fyne.io/tools/cmd/fyne@latest
package-macos:
	$(FYNE) package --os darwin --src $(PKG) --release

package-windows:
	$(FYNE) package --os windows --src $(PKG) --release

package-linux:
	$(FYNE) package --os linux --src $(PKG) --release

clean:
	rm -rf bin dist
