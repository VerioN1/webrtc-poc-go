# Use the official Golang image as the build stage
FROM golang:1.23
# Set the working directory inside the container
WORKDIR /app

# Copy the Go source file into the container
RUN ["go", "install", "github.com/pion/webrtc/v4/examples/reflect@latest"]
WORKDIR /reflect
COPY main.go go.sum go.mod index.html ./
# # Build the Go application
RUN go mod tidy
RUN go build

CMD ["go", "run", "main.go"]