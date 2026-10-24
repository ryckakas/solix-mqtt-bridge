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

FROM gcr.io/distroless/static:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
COPY --from=build /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
