FROM golang:1.27.3-alpine AS build

WORKDIR /src
COPY backend/go.mod backend/go.sum ./backend/
WORKDIR /src/backend
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/livepoll ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/livepoll /app/livepoll
ENV PORT=10000
EXPOSE 10000
ENTRYPOINT ["/app/livepoll"]
