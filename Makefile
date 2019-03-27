.POSIX:
.SUFFIXES:
.PHONY: debug release lint clean version install_vendor editdoc
.SILENT: version 

SOURCES = $(shell find . -type f -name '*.go' -not -path "./vendor/*")

BINARY=vssh
FULL=github.com/stephane-martin/vssh
VERSION=0.2.0
LDFLAGS=-ldflags '-X main.Version=${VERSION}' -gcflags "all=-N -l"
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

README.rst: docs/README.rst
	pandoc -s --toc --toc-depth=1 --wrap=auto --columns=80 --number-sections --from rst --to rst -o README.rst docs/README.rst

editdoc:
	nohup restview docs/README.rst >/dev/null &
	nvim docs/README.rst	
	pkill restview
	pandoc -s --toc --toc-depth=1 --wrap=auto --columns=80 --number-sections --from rst --to rst -o README.rst docs/README.rst

clean:
	rm -f ${BINARY} ${BINARY}_debug

version:
	echo ${VERSION}

lint:
	golangci-lint run -E dupl,goconst,gosec,interfacer,maligned,prealloc,scopelint,unconvert,unparam
