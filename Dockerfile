FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/inventoryd ./cmd/inventoryd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/inventoryd /inventoryd
USER nonroot:nonroot
EXPOSE 19090 18080
ENTRYPOINT ["/inventoryd"]
