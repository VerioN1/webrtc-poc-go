package grpc_server

import (
	"context"
	"io"
	"log"
	"time"
	pb "webrtc_poc_go/pkg/protos"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GrpcServerManager struct {
	ReceiverChan chan []byte
	stream       pb.ImageService_StreamImageClient
	client       pb.ImageServiceClient
	connection   *grpc.ClientConn
	cancel       context.CancelFunc
}

// func runImageService(client pb.ImageServiceClient) {
// 	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
// 	defer cancel()

// 	// Call the streaming API
// 	stream, err := client.StreamImage(ctx)
// 	if err != nil {
// 		log.Fatalf("opennstream error: %v", err)
// 	}

// 	waitc := make(chan struct{})
// 	go func() {
// 		defer close(waitc)
// 		imageCount := 0
// 		for {
// 			in, err := stream.Recv()
// 			if err == io.EOF {
// 				// Server has closed the stream
// 				log.Println("Server closed the stream")
// 				return
// 			}
// 			if err != nil {
// 				log.Fatalf("Failed to receive image: %v", err)
// 			}

// 			// Save the received image
// 			imageCount++
// 			filename := fmt.Sprintf("received_image_%d.jpg", imageCount)
// 			err = os.WriteFile(filename, in.Image.ImageData, 0644)
// 			if err != nil {
// 				log.Printf("Failed to save received image: %v", err)
// 				continue
// 			}
// 			log.Printf("Saved received image to %s", filename)
// 		}
// 	}()

// 	imagePath := "pkg/testing.jpg"
// 	imageData, err := os.ReadFile(imagePath)
// 	if err != nil {
// 		log.Fatalf("Failed to read image file: %v", err)
// 	}

// 	images := []*pb.StreamImageRequest{
// 		{Image: &pb.Image{ImageData: imageData}},
// 		{Image: &pb.Image{ImageData: imageData}},
// 		{Image: &pb.Image{ImageData: imageData}},
// 		{Image: &pb.Image{ImageData: imageData}},
// 	}

// 	for _, note := range images {
// 		log.Println("Sending image to server")
// 		if err := stream.Send(note); err != nil {
// 			log.Fatalf("client.RouteChat: stream.Send(%v) failed: %v", note, err)
// 		}
// 	}

// 	stream.CloseSend()
// 	if err != nil {
// 		log.Fatalf("Failed t send image: %v", err)
// 	}
// 	log.Println("Sent image to server")

// 	<-waitc
// 	log.Println("Client has finished receiving images")
// }

func InitRpcConnection(wsContext context.Context) *GrpcServerManager {
	ctx, cancel := context.WithCancel(wsContext)
	// Remove defer cancel() here

	conn, err := grpc.NewClient("172.27.57.33:50052", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}

	client := pb.NewImageServiceClient(conn)

	GrpcResponseChan := make(chan []byte)
	// Call the streaming API
	stream, err := client.StreamImage(ctx)
	if err != nil {
		log.Fatalf("StreamImage error: %v", err)
	}

	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Printf("Receive error: %v", err)
				break
			}
			GrpcResponseChan <- resp.Image.ImageData
		}
	}()
	grpcManager := &GrpcServerManager{
		ReceiverChan: GrpcResponseChan,
		stream:       stream,
		client:       client,
		connection:   conn,
		cancel:       cancel, // store cancel to close later if needed
	}

	return grpcManager
}

func (g *GrpcServerManager) StreamImage(imageData []byte, isKeyFrame bool) {
	imageToSend := &pb.StreamImageRequest{
		Image:      &pb.Image{ImageData: imageData},
		IsKeyFrame: isKeyFrame,
	}
	startRead := time.Now()
	if err := g.stream.Send(imageToSend); err != nil {
		log.Printf("client.RouteChat: stream.Send failed failed: %v", err)
	}
	log.Printf("send to stream took %v", time.Since(startRead))
}

func (g *GrpcServerManager) Close() {
	log.Printf("Closing gRPC connection")
	g.stream.CloseSend()
	g.connection.Close()
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
