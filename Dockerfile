FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o pi-monitor main.go

FROM alpine

WORKDIR /app

COPY --from=builder /app/pi-monitor /app/

COPY web/ /app/web/

EXPOSE 8083

CMD ["/app/pi-monitor"]
