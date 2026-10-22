FROM golang:1.27.2-alpine AS build

WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/queuectl ./cmd/queuectl

FROM alpine:3.24
RUN addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app
COPY --from=build --chown=10001:10001 /out/server /server
COPY --from=build --chown=10001:10001 /out/worker /worker
COPY --from=build --chown=10001:10001 /out/queuectl /queuectl
USER 10001:10001
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=12 CMD wget -q -O /dev/null "http://127.0.0.1:8080/healthz" || exit 1
ENTRYPOINT ["/server"]