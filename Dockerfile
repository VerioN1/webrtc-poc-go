# Use the official Golang image as the base image
FROM golang:1.23.4-bullseye

RUN apt-get update 
RUN apt-get install -y libvpx-dev libogg-dev libx264-dev libvorbis-dev libva-dev ffmpeg

# Install Air for hot reloading from the new repository
RUN go install github.com/air-verse/air@latest

# Set the working directory inside the container
WORKDIR /app

COPY .git .git
# Copy go.mod and go.sum to the working directory
COPY go.mod go.sum ./
# Download dependencies
RUN go mod download

ENV PKG_CONFIG_PATH=/usr/lib/pkgconfig
# Copy the rest of the application code
COPY . .

RUN go get github.com/xlab/libvpx-go/vpx
# Expose the port your application listens on
RUN go build
EXPOSE 9912
# Start the application using Air
CMD ["air", "-c", ".air.toml"]