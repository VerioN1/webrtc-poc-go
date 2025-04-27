package grpc_service

import (
	"context"
	"fmt"
	"sync"
)

var (
	manager     *ConnectionManager
	managerOnce sync.Once
)

// ConnectionManager provides global access to gRPC connections
type ConnectionManager struct {
	mu          sync.RWMutex
	connections map[string]*GrpcServerManager // Map connectionID -> gRPC connection
}

// GetConnectionManager returns the singleton connection manager
func GetConnectionManager() *ConnectionManager {
	managerOnce.Do(func() {
		manager = &ConnectionManager{
			connections: make(map[string]*GrpcServerManager),
		}
	})
	return manager
}

// InitConnection initializes the gRPC connection for a specific WebSocket
func (c *ConnectionManager) InitConnection(ctx context.Context, connectionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.connections[connectionID]; !exists {
		// Try to create connection and handle potential errors
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Recovered from gRPC connection error: %v\n", r)
				// Make sure the connection is nil in case of panic
				c.connections[connectionID] = nil
			}
		}()

		// Try to initialize the connection
		connection := InitRpcConnection(ctx)
		if connection == nil {
			fmt.Println("Failed to initialize gRPC connection")
		}

		// Store the connection (which may be nil)
		c.connections[connectionID] = connection
	}
}

// GetConnection returns the gRPC connection for a specific WebSocket
func (c *ConnectionManager) GetConnection(connectionID string) *GrpcServerManager {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Check if connection exists in map
	connection, exists := c.connections[connectionID]
	if !exists || connection == nil {
		return nil
	}

	return connection
}

// CloseConnection closes the gRPC connection for a specific WebSocket
func (c *ConnectionManager) CloseConnection(connectionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if conn, exists := c.connections[connectionID]; exists {
		conn.Close()
		delete(c.connections, connectionID)
	}
}
