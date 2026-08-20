# Stage 1: build the frontend → frontend/dist (go:embed consumes it)
FROM oven/bun:1.3.14 AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/bun.lock ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

# Stage 2: build the Go binary
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 go build -o /pavedway .

# Stage 3: runtime
FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=build /pavedway /pavedway
EXPOSE 8080
ENTRYPOINT ["/pavedway", "serve"]
