FROM golang:1.27.4-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN set -eux; \
  for target in server worker routefitmock migrate; do \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o "/out/$target" "./cmd/$target"; \
  done

FROM alpine:3.24
RUN addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app
COPY --from=build --chown=10001:10001 /out/server /server
COPY --from=build --chown=10001:10001 /out/worker /worker
COPY --from=build --chown=10001:10001 /out/routefitmock /routefitmock
COPY --from=build --chown=10001:10001 /out/migrate /migrate
USER 10001:10001
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD sh -c 'port="${HTTP_ADDR:-:8080}"; port="${port##*:}"; wget -q -O /dev/null "http://127.0.0.1:${port}/healthz" || exit 1'
ENTRYPOINT ["/server"]
