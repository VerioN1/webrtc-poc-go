0. generate gRPC :
a. generates the protos in the cwd where the proto file is located(proto folder):

protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative model.proto

<!-- protoc --go_out=. --go-grpc_out=. model.proto -->
-------------------------
1. make sure the index.html server url is configured properly
2. run the docker container
```
docker-compose up -d --build --force-recreate
```
3. you can run the index.html with vscode live server extension