# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build

ARG VERSION=dev
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go test ./... \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/filestore-api ./cmd/filestore-api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/filestore ./cmd/filestore

FROM gcr.io/distroless/static-debian12:nonroot AS api
COPY --from=build /out/filestore-api /filestore-api
EXPOSE 8080
ENTRYPOINT ["/filestore-api"]

FROM gcr.io/distroless/static-debian12:nonroot AS cli
COPY --from=build /out/filestore /filestore
ENTRYPOINT ["/filestore"]
