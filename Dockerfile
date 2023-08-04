# Copyright 2022 Nokia
# Licensed under the Apache License 2.0
# SPDX-License-Identifier: Apache-2.0

# Build the manager binary
FROM golang:1.19 as builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY pkg/ pkg/
COPY controllers/ controllers/

COPY cmd/ cmd/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager main.go

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o iptool cmd/iptool/main.go

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o getnspath cmd/getnspath/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM alpine:latest
RUN apk add --update && \
    apk add --no-cache openssh && \
    apk add curl && \
    apk add tcpdump && \
    apk add iperf3 &&\
    apk add netcat-openbsd && \
    apk add ethtool && \
    apk add bonding && \
    apk add openssh && \
    apk add iproute2 && \
    rm -rf /tmp/*/var/cache/apk/*
RUN curl -sL https://get-gnmic.kmrd.dev | sh
#RUN curl -L https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.26.0/crictl-v1.26.0-linux-amd64.tar.gz --output crictl-v1.26.0-linux-amd64.tar.gz
#RUN tar zxvf crictl-v1.26.0-linux-amd64.tar.gz -C /usr/local/bin
#RUN rm -f crictl-v1.26.0-linux-amd64.tar.gz
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/iptool .
COPY --from=builder /workspace/getnspath .
ENTRYPOINT ["/manager"]
