
export GO111MODULE=on
export GOROOT=$(PWD)

.PHONY: bin
bin: fmt vet
	go build -o bin/pod-dive github.com/caiobegotti/pod-dive/cmd/plugin

.PHONY: test
test:
	go test ./pkg/... ./cmd/... -coverprofile cover.out

.PHONY: fmt
fmt:
	go fmt ./pkg/... ./cmd/...

.PHONY: vet
vet:
	go vet ./pkg/... ./cmd/...

.PHONY: kubernetes-deps
kubernetes-deps:
	go get k8s.io/client-go@v11.0.0
	go get k8s.io/api@kubernetes-1.14.0
	go get k8s.io/apimachinery@kubernetes-1.14.0
	go get k8s.io/cli-runtime@kubernetes-1.14.0

.PHONY: setup
setup:
	make -C setup

# https://github.com/guessi/kubectl-grep/blo

# https://github.com/corneliusweig/ketall/blob/master/Makefile