BUILD_HASH = $(shell git rev-parse HEAD)

build:
	GOOS=linux GOARCH=amd64 go build -o server main.go
	docker build -t wez470/bgg-site:$(BUILD_HASH) .

push:
	docker push wez470/bgg-site:$(BUILD_HASH)

clean:
	rm server

.PHONY: build clean
