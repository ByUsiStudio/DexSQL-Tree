BIN := bin/dexsql-tree
VERSION ?= 1.0.0
LDFLAGS := -s -w -X github.com/byusi-studio/dexsql-tree/internal/api.Version=$(VERSION)

.PHONY: all build test java demo fmt vet clean release cross

all: build

build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/dexsql-tree

# Cross-compile every supported platform (linux/darwin/windows/freebsd,
# amd64/arm64/armv7) into dist/. Pure-Go engine, so no toolchain per target.
release:
	VERSION=$(VERSION) ./scripts/build-all.sh

cross:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /dev/null ./cmd/dexsql-tree
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o /dev/null ./cmd/dexsql-tree
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -o /dev/null ./cmd/dexsql-tree

test:
	go test ./...
	cd java && mvn -q test

java:
	cd java && mvn -q -DskipTests install

demo: build
	go run ./cmd/dexgen -out testdata
	@echo "--- methods"
	@./$(BIN) query --input testdata/demo.apk \
		--sql "SELECT descriptor, access_flags FROM methods ORDER BY descriptor"
	@echo "--- callers of println"
	@./$(BIN) tree --input testdata/demo.dex \
		--method 'Ljava/io/PrintStream;->println(Ljava/lang/String;)V' --depth 2
	@echo "--- smali"
	@./$(BIN) smali --input testdata/demo.dex \
		--method 'Ldemo/Hello;->main([Ljava/lang/String;)V'

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -rf bin dist java/target java/*/target
