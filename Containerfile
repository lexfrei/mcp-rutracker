FROM docker.io/library/golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS builder

ARG VERSION=development
ARG REVISION=unknown

# hadolint ignore=DL3018
RUN echo 'nobody:x:65534:65534:Nobody:/home/nobody:' > /tmp/passwd && \
    apk add --no-cache ca-certificates && \
    mkdir -p /home/nobody/.mcp-rutracker && chown 65534:65534 /home/nobody/.mcp-rutracker

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.revision=${REVISION}" -trimpath ./cmd/mcp-rutracker

FROM scratch

COPY --from=builder /tmp/passwd /etc/passwd
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder --chmod=555 /build/mcp-rutracker /mcp-rutracker
COPY --from=builder --chown=65534:65534 /home/nobody/.mcp-rutracker /home/nobody/.mcp-rutracker

ENV RUTRACKER_COOKIE_FILE=/home/nobody/.mcp-rutracker/cookies.json

USER 65534
ENTRYPOINT ["/mcp-rutracker"]
