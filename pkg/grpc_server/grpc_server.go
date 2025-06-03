package grpc_server

import (
	"context"
	"io"
	"log"
	"net"
	pb "webrtc_poc_go/pkg/protos"

	"google.golang.org/grpc"
)

type GrpcServerManager struct {
	ReceiverChan chan []byte
	server       *grpc.Server
	listener     net.Listener
	cancel       context.CancelFunc
}

// VideoOutputServer implements the gRPC server for receiving video frames
type VideoOutputServer struct {
	pb.UnimplementedVideoOutputServer
	frameReceiver chan []byte
}

// OutputVideoStream handles incoming video stream from clients
func (s *VideoOutputServer) OutputVideoStream(stream grpc.ClientStreamingServer[pb.Request, pb.Response]) error {
	log.Println("Client connected to video stream")

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			log.Println("Client closed the stream")
			// Send final response when client is done
			return stream.SendAndClose(&pb.Response{})
		}
		if err != nil {
			log.Printf("Error receiving frame: %v", err)
			return err
		}

		// Send the received frame to the channel
		select {
		case s.frameReceiver <- req.Frame:
			log.Printf("Received frame of size: %d bytes", len(req.Frame))
		default:
			log.Println("Frame receiver channel is full, dropping frame")
		}
	}
}

func InitRpcConnection(wsContext context.Context) *GrpcServerManager {
	ctx, cancel := context.WithCancel(wsContext)

	lis, err := net.Listen("tcp", ":50061")
	if err != nil {
		log.Printf("Failed to listen on port 50052: %v", err)
		cancel()
		return nil
	}

	// Create gRPC server
	server := grpc.NewServer()

	// Create frame receiver channel
	frameReceiver := make(chan []byte, 100) // Buffered channel to avoid blocking

	// Register the video output service
	videoServer := &VideoOutputServer{
		frameReceiver: frameReceiver,
	}
	pb.RegisterVideoOutputServer(server, videoServer)

	// Start server in goroutine
	go func() {
		log.Println("Starting gRPC server on :50052")
		if err := server.Serve(lis); err != nil {
			log.Printf("Failed to serve gRPC: %v", err)
		}
	}()

	// Handle context cancellation
	go func() {
		<-ctx.Done()
		log.Println("Shutting down gRPC server")
		server.GracefulStop()
	}()

	grpcManager := &GrpcServerManager{
		ReceiverChan: frameReceiver,
		server:       server,
		listener:     lis,
		cancel:       cancel,
	}

	return grpcManager
}

func (g *GrpcServerManager) Close() {
	log.Printf("Closing gRPC server")
	if g.server != nil {
		g.server.GracefulStop()
	}
	if g.listener != nil {
		g.listener.Close()
	}
	if g.cancel != nil {
		g.cancel()
	}
}

// func() {
// 	defer close(GrpcResponseChan)
// 	for {
// 		select {
// 		case <-ctx.Done():
// 			log.Println("Context is done")
// 			return
// 		default:
// 			in, err := stream.Recv()
// 			if err == io.EOF {
// 				log.Println("Server closed the stream")
// 				return
// 			}
// 			if err != nil {
// 				log.Fatalf("Failed to receive image: %v", err)
// 			}
// 			GrpcResponseChan <- in.Image.ImageData
// 		}
// 	}
// }()
// log.Println("Client has finished receiving images")
