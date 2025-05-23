# Build the mounter binary
FROM quay.io/projectquay/golang:1.22 as builder

ARG BUILDVERSION
ARG BUILDCOMMIT

ARG TARGETARCH=amd64
ARG TARGETOS=linux

ENV GOPATH=/go
ENV GO111MODULE=on
ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /go/src/device-mounter

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

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH}  go build -a \
    -ldflags="-X github.com/coldzerofear/device-mounter/pkg/versions.BuildVersion=${BUILDVERSION}_linux-${TARGETARCH} \
    -X github.com/coldzerofear/device-mounter/pkg/versions.BuildCommit=${BUILDCOMMIT}" \
    -o bin/apiserver cmd/apiserver/main.go

FROM quay.io/jitesoft/alpine:latest

WORKDIR /

COPY --from=builder /go/src/device-mounter/bin/apiserver .

USER 1000:1000

ENTRYPOINT ["/apiserver"]
