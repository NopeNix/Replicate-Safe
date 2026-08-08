# syntax=docker/dockerfile:1.6

FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/replicate-safe-frontend ./frontend

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/replicate-safe-frontend /replicate-safe-frontend
EXPOSE 8080
ENTRYPOINT ["/replicate-safe-frontend"]
