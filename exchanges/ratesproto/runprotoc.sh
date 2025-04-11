# Requires protoc, grpc and protoc-gen-go
# To install protoc: https://grpc.io/docs/protoc-installation
# To install grpc and protoc-gen-go:  https://grpc.io/docs/quickstart/go.html#install-grpc
 protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative dcrrates.proto
