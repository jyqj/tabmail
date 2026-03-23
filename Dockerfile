FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /tabmail ./cmd/tabmail \
 && CGO_ENABLED=0 go build -ldflags "-s -w" -o /tabmail-migrate ./cmd/tabmail-migrate

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /tabmail /usr/local/bin/tabmail
COPY --from=builder /tabmail-migrate /usr/local/bin/tabmail-migrate
RUN mkdir -p /data
EXPOSE 8080 2525
ENTRYPOINT ["tabmail"]
