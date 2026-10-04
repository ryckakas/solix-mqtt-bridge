# syntax=docker/dockerfile:1
# check=skip=InvalidDefaultArgInFrom

# No default: the version comes from go.mod's toolchain line (see `make image` and CI).
ARG GO_VERSION

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS build
ARG TARGETOS TARGETARCH TARGETVARIANT
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/solix-openwb-bridge ./cmd/solix-openwb-bridge

FROM gcr.io/distroless/static:nonroot@sha256:f7f8f729987ad0fdf6b05eeeae94b26e6a0f613bdf46feea7fc40f7bd72953e6
COPY --from=build /out/solix-openwb-bridge /solix-openwb-bridge
USER nonroot:nonroot
ENTRYPOINT ["/solix-openwb-bridge"]
