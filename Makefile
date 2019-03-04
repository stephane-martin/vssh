.POSIX:
.SUFFIXES:
.PHONY: debug release vet clean version install_vendor
.SILENT: version 

SOURCES = $(shell find . -type f -name '*.go' -not -path "./vendor/*")

BINARY=vssh
FULL=github.com/stephane-martin/vssh
VERSION=0.1.0
LDFLAGS=-ldflags '-X main.Version=${VERSION}'
LDFLAGS_RELEASE=-ldflags '-w -s -X main.Version=${VERSION}'

debug: ${BINARY}_debug
release: ${BINARY}

install_vendor:
	go install -i ./vendor/...

${BINARY}_debug: ${SOURCES}  
	dep ensure
	CGO_ENABLED=0 go build -x -tags 'netgo osusergo' -o ${BINARY}-debug ${LDFLAGS} ${FULL}

${BINARY}: ${SOURCES}
	dep ensure
	CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY} ${LDFLAGS_RELEASE} ${FULL}

clean:
	rm -f ${BINARY} ${BINARY}_debug

version:
	echo ${VERSION}

vet:
	go vet ./...


