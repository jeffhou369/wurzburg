# ==========================================
# 階段 1: 建置 Angular 前端
# ==========================================
FROM node:22-alpine AS frontend-builder
WORKDIR /build/frontend

COPY src/frontend/package*.json ./
RUN npm ci

COPY src/frontend/ ./
RUN npm run build

# ==========================================
# 階段 2: 建置 Go 後端（將前端靜態資源打包內嵌）
# ==========================================
FROM golang:1.26-alpine AS backend-builder
WORKDIR /build/backend

COPY src/backend/go.mod src/backend/go.sum ./
RUN go mod download

COPY src/backend/ ./
# 將階段 1 產出之前端檔案複製進靜態資源內嵌目錄
COPY --from=frontend-builder /build/frontend/dist/frontend/browser/ ./internal/static/dist/

# 編譯靜態連結之二進位檔
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /wurzburg ./cmd/wurzburg

# ==========================================
# 階段 3: 最小執行映像檔（以非 root 身份執行）
# ==========================================
FROM alpine:3.21

# 建立專用之非特權系統使用者
RUN addgroup -S -g 10001 wurzburg && \
    adduser -S -u 10001 -G wurzburg -s /sbin/nologin wurzburg && \
    apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=backend-builder --chown=wurzburg:wurzburg /wurzburg /app/wurzburg

USER 10001:10001

EXPOSE 8080

ENTRYPOINT ["/app/wurzburg"]
