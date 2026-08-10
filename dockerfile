FROM golang:1.24-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=v1.0.0
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w -X main.AppVersion=${VERSION}" -o domainhunter .
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /build/domainhunter .
RUN mkdir -p /app/data
EXPOSE 8080
CMD ["./domainhunter"]
