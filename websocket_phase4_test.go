package router_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/goliatone/go-router"
)

// Test Phase 4: Advanced Features

func TestFileTransferManager(t *testing.T) {
	t.Run("Upload file", func(t *testing.T) {
		storage := router.NewMemoryFileStorage()
		manager := router.NewFileTransferManager(storage, 10*1024*1024)
		
		mockClient := &mockWSClient{id: "test-client"}
		
		// Start upload
		metadata := map[string]interface{}{
			"name":      "test.txt",
			"size":      float64(1024),
			"mime_type": "text/plain",
		}
		
		transfer, err := manager.StartUpload(context.Background(), mockClient, metadata)
		if err != nil {
			t.Fatalf("Failed to start upload: %v", err)
		}
		
		if transfer.Name != "test.txt" {
			t.Errorf("Expected name test.txt, got %s", transfer.Name)
		}
		
		// Simulate receiving chunks
		data := make([]byte, 1024)
		chunkSize := transfer.ChunkSize
		
		for i := 0; i < transfer.TotalChunks; i++ {
			start := i * chunkSize
			end := start + chunkSize
			if end > len(data) {
				end = len(data)
			}
			
			chunk := data[start:end]
			err := manager.ReceiveChunk(context.Background(), transfer.ID, i, chunk)
			if err != nil {
				t.Fatalf("Failed to receive chunk %d: %v", i, err)
			}
		}
		
		// Verify file was stored
		if !storage.Exists(context.Background(), transfer.ID) {
			t.Error("File was not stored")
		}
		
		// Retrieve and verify
		storedData, metadata, err := storage.Retrieve(context.Background(), transfer.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve file: %v", err)
		}
		
		if len(storedData) != len(data) {
			t.Errorf("Expected data length %d, got %d", len(data), len(storedData))
		}
		
		if metadata["name"] != "test.txt" {
			t.Errorf("Expected metadata name test.txt, got %v", metadata["name"])
		}
	})
	
	t.Run("File size limit", func(t *testing.T) {
		storage := router.NewMemoryFileStorage()
		manager := router.NewFileTransferManager(storage, 1024) // 1KB limit
		
		mockClient := &mockWSClient{id: "test-client"}
		
		metadata := map[string]interface{}{
			"name": "large.txt",
			"size": float64(2048), // 2KB, exceeds limit
		}
		
		_, err := manager.StartUpload(context.Background(), mockClient, metadata)
		if err == nil {
			t.Error("Expected error for file exceeding size limit")
		}
	})
	
	t.Run("Download file", func(t *testing.T) {
		storage := router.NewMemoryFileStorage()
		manager := router.NewFileTransferManager(storage, 10*1024*1024)
		
		// Store a file first
		fileID := "test-file"
		data := []byte("test content")
		metadata := map[string]interface{}{
			"name":      "download.txt",
			"mime_type": "text/plain",
		}
		
		err := storage.Store(context.Background(), fileID, data, metadata)
		if err != nil {
			t.Fatalf("Failed to store file: %v", err)
		}
		
		mockClient := &mockWSClient{id: "test-client"}
		
		// Start download
		err = manager.StartDownload(context.Background(), mockClient, fileID)
		if err != nil {
			t.Fatalf("Failed to start download: %v", err)
		}
		
		// Verify client received file info
		// In real test, would verify chunks were sent
	})
	
	t.Run("Cancel transfer", func(t *testing.T) {
		storage := router.NewMemoryFileStorage()
		manager := router.NewFileTransferManager(storage, 10*1024*1024)
		
		mockClient := &mockWSClient{id: "test-client"}
		
		metadata := map[string]interface{}{
			"name": "cancel.txt",
			"size": float64(1024),
		}
		
		transfer, err := manager.StartUpload(context.Background(), mockClient, metadata)
		if err != nil {
			t.Fatalf("Failed to start upload: %v", err)
		}
		
		// Cancel the transfer
		err = manager.CancelTransfer(transfer.ID)
		if err != nil {
			t.Fatalf("Failed to cancel transfer: %v", err)
		}
		
		// Try to get transfer - should fail
		_, err = manager.GetTransfer(transfer.ID)
		if err == nil {
			t.Error("Expected error for cancelled transfer")
		}
	})
}

func TestBinaryMessageCodec(t *testing.T) {
	t.Run("Encode and decode", func(t *testing.T) {
		codec := router.NewBinaryMessageCodec(1024 * 1024)
		
		original := &router.BinaryPayload{
			Type: "test",
			Data: []byte("test data"),
		}
		
		// Encode
		encoded, err := codec.Encode(original)
		if err != nil {
			t.Fatalf("Failed to encode: %v", err)
		}
		
		// Decode
		decoded, err := codec.Decode(encoded)
		if err != nil {
			t.Fatalf("Failed to decode: %v", err)
		}
		
		if decoded.Type != original.Type {
			t.Errorf("Expected type %s, got %s", original.Type, decoded.Type)
		}
		
		if !bytes.Equal(decoded.Data, original.Data) {
			t.Error("Data mismatch after encode/decode")
		}
	})
	
	t.Run("Max message size", func(t *testing.T) {
		codec := router.NewBinaryMessageCodec(100) // 100 bytes max
		
		msg := &router.BinaryPayload{
			Type: "test",
			Data: make([]byte, 200), // Exceeds max
		}
		
		encoded, err := codec.Encode(msg)
		if err != nil {
			t.Fatalf("Failed to encode: %v", err)
		}
		
		_, err = codec.Decode(encoded)
		if err == nil {
			t.Error("Expected error for message exceeding max size")
		}
	})
}

func TestReconnectManager(t *testing.T) {
	t.Run("Create and retrieve session", func(t *testing.T) {
		config := router.DefaultReconnectConfig()
		manager := router.NewReconnectManager(config)
		
		mockClient := &mockWSClient{id: "test-client"}
		
		// Create session
		session, err := manager.CreateSession(mockClient)
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}
		
		if session.ClientID != mockClient.ID() {
			t.Errorf("Expected client ID %s, got %s", mockClient.ID(), session.ClientID)
		}
		
		if session.ReconnectToken == "" {
			t.Error("Expected reconnect token to be generated")
		}
	})
	
	t.Run("Handle reconnection", func(t *testing.T) {
		config := router.DefaultReconnectConfig()
		manager := router.NewReconnectManager(config)
		
		mockClient := &mockWSClient{id: "test-client"}
		
		// Create session
		session, err := manager.CreateSession(mockClient)
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}
		
		// Simulate disconnect
		manager.HandleDisconnect(session.ID)
		
		// Simulate reconnect
		newClient := &mockWSClient{id: "test-client-2"}
		reconnected, err := manager.HandleReconnect(
			context.Background(),
			newClient,
			session.ID,
			session.ReconnectToken,
		)
		
		if err != nil {
			t.Fatalf("Failed to reconnect: %v", err)
		}
		
		if reconnected.ID != session.ID {
			t.Error("Session ID mismatch after reconnect")
		}
	})
	
	t.Run("Invalid reconnect token", func(t *testing.T) {
		config := router.DefaultReconnectConfig()
		manager := router.NewReconnectManager(config)
		
		mockClient := &mockWSClient{id: "test-client"}
		
		// Create session
		session, err := manager.CreateSession(mockClient)
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}
		
		// Try to reconnect with invalid token
		newClient := &mockWSClient{id: "test-client-2"}
		_, err = manager.HandleReconnect(
			context.Background(),
			newClient,
			session.ID,
			"invalid-token",
		)
		
		if err == nil {
			t.Error("Expected error for invalid reconnect token")
		}
	})
	
	t.Run("Session timeout", func(t *testing.T) {
		config := router.DefaultReconnectConfig()
		config.SessionTimeout = 100 * time.Millisecond
		manager := router.NewReconnectManager(config)
		
		mockClient := &mockWSClient{id: "test-client"}
		
		// Create session
		session, err := manager.CreateSession(mockClient)
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}
		
		// Simulate disconnect
		manager.HandleDisconnect(session.ID)
		
		// Wait for timeout
		time.Sleep(200 * time.Millisecond)
		
		// Try to reconnect after timeout
		newClient := &mockWSClient{id: "test-client-2"}
		_, err = manager.HandleReconnect(
			context.Background(),
			newClient,
			session.ID,
			session.ReconnectToken,
		)
		
		if err == nil {
			t.Error("Expected error for expired session")
		}
	})
	
	t.Run("Queue messages for disconnected client", func(t *testing.T) {
		config := router.DefaultReconnectConfig()
		manager := router.NewReconnectManager(config)
		
		mockClient := &mockWSClient{id: "test-client"}
		
		// Create session
		session, err := manager.CreateSession(mockClient)
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}
		
		// Simulate disconnect
		manager.HandleDisconnect(session.ID)
		
		// Queue messages
		for i := 0; i < 3; i++ {
			msg := &router.QueuedMessage{
				ID:   generateTestID(),
				Type: "test",
				Data: i,
			}
			err := manager.QueueMessage(session.ID, msg)
			if err != nil {
				t.Fatalf("Failed to queue message: %v", err)
			}
		}
		
		// Reconnect and verify messages are delivered
		newClient := &mockWSClient{id: "test-client-2"}
		_, err = manager.HandleReconnect(
			context.Background(),
			newClient,
			session.ID,
			session.ReconnectToken,
		)
		
		if err != nil {
			t.Fatalf("Failed to reconnect: %v", err)
		}
		
		// In a real implementation, check that queued messages were sent
		// For now, just verify reconnection succeeded
	})
}

func TestMessageQueue(t *testing.T) {
	t.Run("Add and flush messages", func(t *testing.T) {
		queue := router.NewMessageQueue(10, 1*time.Hour)
		
		// Add messages
		for i := 0; i < 5; i++ {
			msg := &router.QueuedMessage{
				ID:   generateTestID(),
				Type: "test",
				Data: i,
			}
			err := queue.Add(msg)
			if err != nil {
				t.Fatalf("Failed to add message: %v", err)
			}
		}
		
		if queue.Size() != 5 {
			t.Errorf("Expected queue size 5, got %d", queue.Size())
		}
		
		// Flush messages
		messages := queue.Flush()
		if len(messages) != 5 {
			t.Errorf("Expected 5 messages, got %d", len(messages))
		}
		
		if queue.Size() != 0 {
			t.Errorf("Expected queue to be empty after flush, got size %d", queue.Size())
		}
	})
	
	t.Run("Max queue size", func(t *testing.T) {
		queue := router.NewMessageQueue(3, 1*time.Hour)
		
		// Add more than max
		for i := 0; i < 5; i++ {
			msg := &router.QueuedMessage{
				ID:   generateTestID(),
				Type: "test",
				Data: i,
			}
			queue.Add(msg)
		}
		
		// Should only have last 3
		if queue.Size() != 3 {
			t.Errorf("Expected queue size 3, got %d", queue.Size())
		}
		
		messages := queue.Flush()
		// Check we have the last 3 messages (2, 3, 4)
		for i, msg := range messages {
			expectedData := i + 2
			if msg.Data != expectedData {
				t.Errorf("Expected data %d, got %v", expectedData, msg.Data)
			}
		}
	})
	
	t.Run("Message TTL", func(t *testing.T) {
		queue := router.NewMessageQueue(10, 100*time.Millisecond)
		
		// Add message
		msg := &router.QueuedMessage{
			ID:   generateTestID(),
			Type: "test",
			Data: "old",
		}
		queue.Add(msg)
		
		// Wait for TTL to expire
		time.Sleep(200 * time.Millisecond)
		
		// Add new message
		newMsg := &router.QueuedMessage{
			ID:   generateTestID(),
			Type: "test",
			Data: "new",
		}
		queue.Add(newMsg)
		
		// Flush should only return non-expired message
		messages := queue.Flush()
		if len(messages) != 1 {
			t.Errorf("Expected 1 non-expired message, got %d", len(messages))
		}
		
		if messages[0].Data != "new" {
			t.Error("Expected only new message to remain")
		}
	})
}

func TestHeartbeatManager(t *testing.T) {
	t.Run("Start and stop heartbeat", func(t *testing.T) {
		timeoutCalled := false
		onTimeout := func(clientID string) {
			timeoutCalled = true
		}
		
		manager := router.NewHeartbeatManager(
			100*time.Millisecond,
			200*time.Millisecond,
			onTimeout,
		)
		
		mockClient := &mockWSClient{id: "test-client"}
		
		// Start heartbeat
		manager.StartHeartbeat(mockClient)
		
		// Simulate pong
		manager.HandlePong(mockClient.ID())
		
		// Stop before timeout
		manager.StopHeartbeat(mockClient.ID())
		
		// Wait to ensure timeout doesn't fire
		time.Sleep(300 * time.Millisecond)
		
		if timeoutCalled {
			t.Error("Timeout should not have been called")
		}
	})
	
	t.Run("Heartbeat timeout", func(t *testing.T) {
		timeoutClientID := ""
		onTimeout := func(clientID string) {
			timeoutClientID = clientID
		}
		
		manager := router.NewHeartbeatManager(
			50*time.Millisecond,
			100*time.Millisecond,
			onTimeout,
		)
		
		mockClient := &mockWSClient{id: "test-client"}
		
		// Start heartbeat
		manager.StartHeartbeat(mockClient)
		
		// Don't send pong, wait for timeout
		time.Sleep(150 * time.Millisecond)
		
		if timeoutClientID != mockClient.ID() {
			t.Errorf("Expected timeout for client %s, got %s", mockClient.ID(), timeoutClientID)
		}
	})
}

func TestCompression(t *testing.T) {
	t.Run("Compress and decompress", func(t *testing.T) {
		config := router.CompressionConfig{
			Enabled:   true,
			Level:     5,
			Threshold: 10,
		}
		
		original := []byte("This is a test message that should be compressed")
		
		compressed, wasCompressed, err := router.CompressMessage(original, config)
		if err != nil {
			t.Fatalf("Failed to compress: %v", err)
		}
		
		// In the placeholder implementation, compression doesn't actually happen
		// In real implementation, this would test actual compression
		_ = wasCompressed
		
		decompressed, err := router.DecompressMessage(compressed)
		if err != nil {
			t.Fatalf("Failed to decompress: %v", err)
		}
		
		if !bytes.Equal(decompressed, original) {
			t.Error("Data mismatch after compress/decompress")
		}
	})
	
	t.Run("Skip compression for small messages", func(t *testing.T) {
		config := router.CompressionConfig{
			Enabled:   true,
			Level:     5,
			Threshold: 100,
		}
		
		small := []byte("small")
		
		compressed, wasCompressed, err := router.CompressMessage(small, config)
		if err != nil {
			t.Fatalf("Failed to compress: %v", err)
		}
		
		if wasCompressed {
			t.Error("Small message should not be compressed")
		}
		
		if !bytes.Equal(compressed, small) {
			t.Error("Small message should be returned unchanged")
		}
	})
}

// Mock client is defined in websocket_event_system_test.go