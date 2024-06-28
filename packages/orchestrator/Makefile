.PHONY: build
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/orchestrator .

grpc-gen:
	protoc --go_out=../shared/grpc/orchestrator  --go_opt=paths=source_relative \
		--go-grpc_out=../shared/grpc/orchestrator --go-grpc_opt=paths=source_relative \
		"orchestrator.proto"

