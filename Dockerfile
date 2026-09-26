# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /out/curio ./cmd/curio

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -S -g 10001 curio && adduser -S -D -H -u 10001 -G curio curio && mkdir /data && chown curio:curio /data && chmod 700 /data
COPY --from=build /out/curio /usr/local/bin/curio
USER 10001:10001
ENV CURIO_PORT=8080 CURIO_DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["curio", "healthcheck"]
ENTRYPOINT ["curio"]
