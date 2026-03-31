.PHONY: run test build lint docker

run:
	go run cmd/server/main.go

test:
	go test -race -v ./...

build:
	go build -o bin/quiz-server cmd/server/main.go

lint:
	golangci-lint run

docker:
	docker-compose up --build
