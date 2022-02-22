FROM golang:1.17.6 AS build

WORKDIR /go/src/app

# Add all necessary sources
COPY go.mod .
COPY go.sum .

# Download dependencies
RUN go get -d -v ./...

# Disabled cgo
ENV CGO_ENABLED=0

COPY main.go .

# Build a statically linked binary
RUN go build -a -o main main.go

FROM alpine:3.7 AS prod

RUN mkdir -p /app
WORKDIR /app

COPY --from=build /go/src/app/main /app

COPY templates /app/templates

EXPOSE 80

CMD ["./main"]