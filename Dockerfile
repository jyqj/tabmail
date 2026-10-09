FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
COPY third_party/enmime-v2.3.0/go.mod ./third_party/enmime-v2.3.0/go.mod
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /tabmail ./cmd/tabmail

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /tabmail /usr/local/bin/tabmail
RUN mkdir -p /data
EXPOSE 8080 2525
ENTRYPOINT ["tabmail"]
