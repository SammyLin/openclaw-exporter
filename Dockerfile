FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /openclaw-exporter ./cmd/openclaw-exporter

FROM alpine:3.19

RUN addgroup -g 1000 exporter && \
    adduser -u 1000 -G exporter -s /bin/sh -D exporter

COPY --from=builder /openclaw-exporter /usr/local/bin/openclaw-exporter

USER exporter

EXPOSE 9101

ENTRYPOINT ["openclaw-exporter"]
