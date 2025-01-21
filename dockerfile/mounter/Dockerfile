# Build the mounter binary
FROM quay.io/jitesoft/ubuntu:20.04 as builder

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

ARG BUILDVERSION
ARG BUILDCOMMIT

WORKDIR /go/src/device-mounter

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd cmd/
COPY pkg pkg/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.

RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH}  \
    CGO_CFLAGS="-fstack-protector-strong -D_FORTIFY_SOURCE=2 -O2 -fPIC -ftrapv"  \
    CGO_CPPFLAGS="-fstack-protector-strong -D_FORTIFY_SOURCE=2 -O2 -fPIC -ftrapv"  \
    CGO_LDFLAGS_ALLOW='-Wl,--unresolved-symbols=ignore-in-object-files'  \
    go build -ldflags="-extldflags=-Wl,-z,lazy,-z,relro,-z,noexecstack \
    -X github.com/coldzerofear/device-mounter/pkg/versions.BuildVersion=${BUILDVERSION}_linux-${TARGETARCH} \
    -X github.com/coldzerofear/device-mounter/pkg/versions.BuildCommit=${BUILDCOMMIT}" \
    -o bin/mounter cmd/mounter/main.go

FROM quay.io/jitesoft/ubuntu:24.04

WORKDIR /

COPY --from=builder /go/src/device-mounter/bin/mounter .

COPY scripts scripts/
RUN chmod +x scripts/*

ENTRYPOINT ["/mounter"]