APP_NAME=fyrtaarn
BUILD_DIR=dist

.PHONY: build run tidy clean

build:
	mkdir -p $(BUILD_DIR)
	go build -v -o $(BUILD_DIR)/$(APP_NAME) ./cmd/fyrtaarn

run:
	go run ./cmd/fyrtaarn

tidy:
	go mod tidy

clean:
	rm -rf $(BUILD_DIR)
