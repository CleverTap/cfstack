VERSION=$(shell git describe --tags)
BUCKET=production-global-management-eu-west-1-static-packages

local: clean
	mkdir -p dist
	GOOS=darwin GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o ./dist/cfstack ./cmd/cfstack
	cp ./dist/cfstack ${HOME}/
.PHONY: local

build: clean
	mkdir -p dist/cfstack-${VERSION}-linux-amd64
	GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o ./dist/cfstack-${VERSION}-linux-amd64/cfstack ./cmd/cfstack
.PHONY: build

release: build
	tar cvzf cfstack-${VERSION}-linux-amd64.tar.gz -C ./dist/cfstack-${VERSION}-linux-amd64 .
.PHONY: release

deploy: release
	aws s3 cp cfstack-${VERSION}-linux-amd64.tar.gz s3://${BUCKET}/Cfstack/cfstack-${VERSION}-linux-amd64.tar.gz --acl public-read --profile ${PROFILE}
	rm cfstack-${VERSION}-linux-amd64.tar.gz

clean:
	rm -rf dist/
.PHONY: clean