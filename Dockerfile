# syntax=docker/dockerfile:1.7

FROM golang:1.24 AS build
WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/lineasrv ./cmd/lineasrv

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/lineasrv /app/lineasrv

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/lineasrv"]
