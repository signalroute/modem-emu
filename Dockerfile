FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o modem-emu ./cmd/emu

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /app/modem-emu /modem-emu
EXPOSE 8888
ENTRYPOINT ["/modem-emu"]
