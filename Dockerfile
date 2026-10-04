# syntax=docker/dockerfile:1
# check=skip=InvalidDefaultArgInFrom

# Builds from source (make image, CI, dev stack). Releases use Dockerfile.release with goreleaser's binaries.
# No default: the version comes from go.mod's toolchain line (see `make image` and CI).
ARG GO_VERSION

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS build
ARG TARGETOS TARGETARCH TARGETVARIANT
ARG VERSION=dev
# Any binary under cmd/; the dev stack also builds solarbank-sim and openwb-fake from this file.
ARG CMD=solix-mqtt-bridge
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/app ./cmd/${CMD}

FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=build /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
