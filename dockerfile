# 前端产物 web/dist 随仓库提交，因此镜像只需要 Go 工具链：
# 构建更快，也避免在 arm64（树莓派）上跑一遍 Node 构建。
# 修改前端后请先在 web/ 执行 npm run build 再提交。
FROM golang:1.24-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=v2.11.0
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.AppVersion=${VERSION}" \
    -o domainhunter ./cmd/domainhunter

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata wget
WORKDIR /app
COPY --from=builder /build/domainhunter .
RUN mkdir -p /app/data
EXPOSE 8080
HEALTHCHECK --interval=60s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/health >/dev/null || exit 1
CMD ["./domainhunter"]
