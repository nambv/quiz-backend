FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /quiz-server cmd/server/main.go

FROM alpine:3.20
COPY --from=builder /quiz-server /quiz-server
EXPOSE 8080
ENTRYPOINT ["/quiz-server"]
