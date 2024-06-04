# Build the mounter binary
FROM ubuntu:20.04 as builder

ARG TARGETARCH=amd64
ARG TARGETOS=linux

# use golang version
ARG GOLANG_VERSION=1.22.3

# install compilation environment
RUN apt-get update && apt-get install -y --no-install-recommends g++ ca-certificates wget && \
    rm -rf /var/lib/apt/lists/*

# downlown golang
RUN wget -nv -O - https://golang.google.cn/dl/go${GOLANG_VERSION}.${TARGETOS}-${TARGETARCH}.tar.gz \
    | tar -C /usr/local -xz

ENV GOPATH /go
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH
ENV GO111MODULE on
ENV GOPROXY https://goproxy.cn,direct

WORKDIR /go/src/k8s-device-mounter

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/ cmd/
COPY pkg/ pkg/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o bin/apiserver cmd/apiserver/main.go
RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH}  \
    CGO_CFLAGS="-fstack-protector-strong -D_FORTIFY_SOURCE=2 -O2 -fPIC -ftrapv"  \
    CGO_CPPFLAGS="-fstack-protector-strong -D_FORTIFY_SOURCE=2 -O2 -fPIC -ftrapv"  \
    CGO_LDFLAGS_ALLOW='-Wl,--unresolved-symbols=ignore-in-object-files'  \
    go build -ldflags="-extldflags=-Wl,-z,lazy,-z,relro,-z,noexecstack" -o bin/mounter cmd/mounter/main.go

FROM ubuntu:24.04

WORKDIR /

COPY --from=builder /go/src/k8s-device-mounter/bin/apiserver .
COPY --from=builder /go/src/k8s-device-mounter/bin/mounter .