FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o share-service .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/share-service /usr/local/bin/share-service
EXPOSE 80
ENTRYPOINT ["share-service"]
CMD ["-data", "/data", "-base-url", "https://share.eregistrations.dev"]
