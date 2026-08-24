# syntax=docker/dockerfile:1
# Fluid Debian execution agent — see code/actions/templates/Dockerfile.go-workload

FROM golang:1.27-bookworm AS build

ARG BINARY_NAME=fluid-agent-debian
ARG VERSION=0.0.0
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
COPY core ./core
COPY cmd ./cmd
COPY internal ./internal
COPY config ./config

RUN go mod download

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags "-s -w -X main.Version=${VERSION}" \
        -o /out/workload ./cmd

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/workload /usr/local/bin/fluid-agent-debian

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/fluid-agent-debian"]
CMD ["-config", "/etc/fluid/debian/agent.yml", "-credentials", "/etc/fluid/debian/credentials.yaml", "-enrollment-env", "/etc/fluid/debian/enrollment.env"]
