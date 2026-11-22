BIN := bin/dexsql-tree

.PHONY: all build test java demo fmt vet clean

all: build

build:
	@mkdir -p bin
	go build -o $(BIN) ./cmd/dexsql-tree

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
	rm -rf bin java/target java/*/target
