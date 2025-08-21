package router

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// BinaryPayload represents a binary WebSocket message with metadata
type BinaryPayload struct {
	Type      string                 `json:"type"`
	ID        string                 `json:"id"`
	Data      []byte                 `json:"-"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// BinaryHandler handles binary messages
type BinaryHandler func(ctx context.Context, client WSClient, msg *BinaryPayload) error

// FileTransfer represents an active file transfer
type FileTransfer struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	MimeType    string    `json:"mime_type"`
	Progress    int64     `json:"progress"`
	State       string    `json:"state"` // "pending", "active", "completed", "failed"
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time,omitempty"`
	Error       string    `json:"error,omitempty"`
	Checksum    string    `json:"checksum,omitempty"`
	ChunkSize   int       `json:"chunk_size"`
	TotalChunks int       `json:"total_chunks"`

	// Internal fields
	chunks     map[int][]byte
	chunksMu   sync.RWMutex
	onProgress func(transfer *FileTransfer)
	onComplete func(transfer *FileTransfer, data []byte)
	onError    func(transfer *FileTransfer, err error)
}

// FileTransferManager manages file uploads and downloads
type FileTransferManager struct {
	transfers   map[string]*FileTransfer
	transfersMu sync.RWMutex

	maxFileSize   int64
	maxConcurrent int
	chunkSize     int
	timeout       time.Duration

	// Storage backend
	storage FileStorage
}

// FileStorage interface for storing transferred files
type FileStorage interface {
	Store(ctx context.Context, id string, data []byte, metadata map[string]interface{}) error
	Retrieve(ctx context.Context, id string) ([]byte, map[string]interface{}, error)
	Delete(ctx context.Context, id string) error
	Exists(ctx context.Context, id string) bool
}

// MemoryFileStorage stores files in memory
type MemoryFileStorage struct {
	files   map[string]*storedFile
	filesMu sync.RWMutex
}

type storedFile struct {
	data     []byte
	metadata map[string]interface{}
}

// NewFileTransferManager creates a new file transfer manager
func NewFileTransferManager(storage FileStorage, maxFileSize int64) *FileTransferManager {
	if maxFileSize == 0 {
		maxFileSize = 100 * 1024 * 1024 // 100MB default
	}

	return &FileTransferManager{
		transfers:     make(map[string]*FileTransfer),
		maxFileSize:   maxFileSize,
		maxConcurrent: 10,
		chunkSize:     64 * 1024, // 64KB chunks
		timeout:       5 * time.Minute,
		storage:       storage,
	}
}

// StartUpload initiates a file upload
func (m *FileTransferManager) StartUpload(ctx context.Context, client WSClient, metadata map[string]interface{}) (*FileTransfer, error) {
	// Extract file info from metadata
	name, _ := metadata["name"].(string)
	size, _ := metadata["size"].(float64)
	mimeType, _ := metadata["mime_type"].(string)

	if size > float64(m.maxFileSize) {
		return nil, fmt.Errorf("file size %.2fMB exceeds maximum %.2fMB",
			size/(1024*1024), float64(m.maxFileSize)/(1024*1024))
	}

	// Check concurrent transfers
	m.transfersMu.RLock()
	activeCount := 0
	for _, t := range m.transfers {
		if t.State == "active" {
			activeCount++
		}
	}
	m.transfersMu.RUnlock()

	if activeCount >= m.maxConcurrent {
		return nil, errors.New("maximum concurrent transfers reached")
	}

	// Create transfer
	transfer := &FileTransfer{
		ID:          generateID(),
		Name:        name,
		Size:        int64(size),
		MimeType:    mimeType,
		State:       "pending",
		StartTime:   time.Now(),
		ChunkSize:   m.chunkSize,
		TotalChunks: int((int64(size) + int64(m.chunkSize) - 1) / int64(m.chunkSize)),
		chunks:      make(map[int][]byte),
	}

	// Store transfer
	m.transfersMu.Lock()
	m.transfers[transfer.ID] = transfer
	m.transfersMu.Unlock()

	// Start timeout timer
	go m.monitorTransfer(transfer)

	// Notify client
	client.SendJSON(map[string]interface{}{
		"type":         "upload_ready",
		"transfer_id":  transfer.ID,
		"chunk_size":   transfer.ChunkSize,
		"total_chunks": transfer.TotalChunks,
	})

	return transfer, nil
}

// ReceiveChunk processes an uploaded chunk
func (m *FileTransferManager) ReceiveChunk(ctx context.Context, transferID string, chunkIndex int, data []byte) error {
	m.transfersMu.RLock()
	transfer, exists := m.transfers[transferID]
	m.transfersMu.RUnlock()

	if !exists {
		return fmt.Errorf("transfer %s not found", transferID)
	}

	if transfer.State != "pending" && transfer.State != "active" {
		return fmt.Errorf("transfer %s is not active", transferID)
	}

	// Update state
	if transfer.State == "pending" {
		transfer.State = "active"
	}

	// Store chunk
	transfer.chunksMu.Lock()
	transfer.chunks[chunkIndex] = data
	transfer.Progress += int64(len(data))
	transfer.chunksMu.Unlock()

	// Call progress callback
	if transfer.onProgress != nil {
		transfer.onProgress(transfer)
	}

	// Check if complete
	if len(transfer.chunks) == transfer.TotalChunks {
		return m.completeTransfer(ctx, transfer)
	}

	return nil
}

// completeTransfer finalizes a file transfer
func (m *FileTransferManager) completeTransfer(ctx context.Context, transfer *FileTransfer) error {
	// Assemble chunks
	var buffer bytes.Buffer
	for i := 0; i < transfer.TotalChunks; i++ {
		chunk, exists := transfer.chunks[i]
		if !exists {
			transfer.State = "failed"
			transfer.Error = fmt.Sprintf("missing chunk %d", i)
			return fmt.Errorf("missing chunk %d", i)
		}
		buffer.Write(chunk)
	}

	data := buffer.Bytes()

	// Store file
	metadata := map[string]interface{}{
		"name":      transfer.Name,
		"size":      transfer.Size,
		"mime_type": transfer.MimeType,
		"checksum":  transfer.Checksum,
	}

	if err := m.storage.Store(ctx, transfer.ID, data, metadata); err != nil {
		transfer.State = "failed"
		transfer.Error = err.Error()
		return err
	}

	// Update transfer state
	transfer.State = "completed"
	transfer.EndTime = time.Now()

	// Call complete callback
	if transfer.onComplete != nil {
		transfer.onComplete(transfer, data)
	}

	// Clean up chunks
	transfer.chunks = nil

	return nil
}

// StartDownload initiates a file download
func (m *FileTransferManager) StartDownload(ctx context.Context, client WSClient, fileID string) error {
	// Retrieve file
	data, metadata, err := m.storage.Retrieve(ctx, fileID)
	if err != nil {
		return fmt.Errorf("failed to retrieve file: %w", err)
	}

	// Create transfer
	transfer := &FileTransfer{
		ID:          generateID(),
		Name:        metadata["name"].(string),
		Size:        int64(len(data)),
		MimeType:    metadata["mime_type"].(string),
		State:       "active",
		StartTime:   time.Now(),
		ChunkSize:   m.chunkSize,
		TotalChunks: (len(data) + m.chunkSize - 1) / m.chunkSize,
	}

	// Store transfer
	m.transfersMu.Lock()
	m.transfers[transfer.ID] = transfer
	m.transfersMu.Unlock()

	// Send file info
	client.SendJSON(map[string]interface{}{
		"type":         "download_ready",
		"transfer_id":  transfer.ID,
		"name":         transfer.Name,
		"size":         transfer.Size,
		"mime_type":    transfer.MimeType,
		"chunk_size":   transfer.ChunkSize,
		"total_chunks": transfer.TotalChunks,
	})

	// Send chunks
	go m.sendChunks(ctx, client, transfer, data)

	return nil
}

// sendChunks sends file chunks to client
func (m *FileTransferManager) sendChunks(ctx context.Context, client WSClient, transfer *FileTransfer, data []byte) {
	for i := 0; i < transfer.TotalChunks; i++ {
		start := i * transfer.ChunkSize
		end := start + transfer.ChunkSize
		if end > len(data) {
			end = len(data)
		}

		chunk := data[start:end]

		// Create chunk message
		msg := &BinaryPayload{
			Type: "file_chunk",
			ID:   transfer.ID,
			Data: chunk,
			Metadata: map[string]interface{}{
				"chunk_index":  i,
				"total_chunks": transfer.TotalChunks,
				"final":        i == transfer.TotalChunks-1,
			},
			Timestamp: time.Now(),
		}

		// Send chunk
		if err := m.sendBinaryMessage(client, msg); err != nil {
			transfer.State = "failed"
			transfer.Error = err.Error()
			if transfer.onError != nil {
				transfer.onError(transfer, err)
			}
			return
		}

		transfer.Progress += int64(len(chunk))

		// Small delay between chunks to avoid overwhelming client
		time.Sleep(10 * time.Millisecond)
	}

	// Mark complete
	transfer.State = "completed"
	transfer.EndTime = time.Now()

	// Send completion message
	client.SendJSON(map[string]interface{}{
		"type":        "download_complete",
		"transfer_id": transfer.ID,
	})
}

// sendBinaryMessage sends a binary message with header
func (m *FileTransferManager) sendBinaryMessage(client WSClient, msg *BinaryPayload) error {
	// Create header with metadata
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(msg.Data)))

	// Combine header and data
	payload := append(header, msg.Data...)

	return client.Send(payload)
}

// monitorTransfer monitors a transfer for timeout
func (m *FileTransferManager) monitorTransfer(transfer *FileTransfer) {
	timer := time.NewTimer(m.timeout)
	defer timer.Stop()

	<-timer.C

	if transfer.State == "pending" || transfer.State == "active" {
		transfer.State = "failed"
		transfer.Error = "transfer timeout"

		if transfer.onError != nil {
			transfer.onError(transfer, errors.New("transfer timeout"))
		}

		// Clean up
		m.transfersMu.Lock()
		delete(m.transfers, transfer.ID)
		m.transfersMu.Unlock()
	}
}

// CancelTransfer cancels an active transfer
func (m *FileTransferManager) CancelTransfer(transferID string) error {
	m.transfersMu.Lock()
	defer m.transfersMu.Unlock()

	transfer, exists := m.transfers[transferID]
	if !exists {
		return fmt.Errorf("transfer %s not found", transferID)
	}

	transfer.State = "failed"
	transfer.Error = "cancelled by user"
	transfer.EndTime = time.Now()

	delete(m.transfers, transferID)

	return nil
}

// GetTransfer returns transfer status
func (m *FileTransferManager) GetTransfer(transferID string) (*FileTransfer, error) {
	m.transfersMu.RLock()
	defer m.transfersMu.RUnlock()

	transfer, exists := m.transfers[transferID]
	if !exists {
		return nil, fmt.Errorf("transfer %s not found", transferID)
	}

	return transfer, nil
}

// MemoryFileStorage implementation

// NewMemoryFileStorage creates a new memory file storage
func NewMemoryFileStorage() *MemoryFileStorage {
	return &MemoryFileStorage{
		files: make(map[string]*storedFile),
	}
}

// Store stores a file in memory
func (s *MemoryFileStorage) Store(ctx context.Context, id string, data []byte, metadata map[string]interface{}) error {
	s.filesMu.Lock()
	defer s.filesMu.Unlock()

	s.files[id] = &storedFile{
		data:     data,
		metadata: metadata,
	}

	return nil
}

// Retrieve retrieves a file from memory
func (s *MemoryFileStorage) Retrieve(ctx context.Context, id string) ([]byte, map[string]interface{}, error) {
	s.filesMu.RLock()
	defer s.filesMu.RUnlock()

	file, exists := s.files[id]
	if !exists {
		return nil, nil, fmt.Errorf("file %s not found", id)
	}

	return file.data, file.metadata, nil
}

// Delete deletes a file from memory
func (s *MemoryFileStorage) Delete(ctx context.Context, id string) error {
	s.filesMu.Lock()
	defer s.filesMu.Unlock()

	delete(s.files, id)
	return nil
}

// Exists checks if a file exists
func (s *MemoryFileStorage) Exists(ctx context.Context, id string) bool {
	s.filesMu.RLock()
	defer s.filesMu.RUnlock()

	_, exists := s.files[id]
	return exists
}

// BinaryMessageCodec encodes/decodes binary messages
type BinaryMessageCodec struct {
	maxMessageSize int64
}

// NewBinaryMessageCodec creates a new binary message codec
func NewBinaryMessageCodec(maxSize int64) *BinaryMessageCodec {
	if maxSize == 0 {
		maxSize = 10 * 1024 * 1024 // 10MB default
	}
	return &BinaryMessageCodec{
		maxMessageSize: maxSize,
	}
}

// Encode encodes a binary message
func (c *BinaryMessageCodec) Encode(msg *BinaryPayload) ([]byte, error) {
	// Simple format: [type_len][type][data_len][data]
	var buf bytes.Buffer

	// Write type length and type
	typeBytes := []byte(msg.Type)
	if err := binary.Write(&buf, binary.BigEndian, uint32(len(typeBytes))); err != nil {
		return nil, err
	}
	buf.Write(typeBytes)

	// Write data length and data
	if err := binary.Write(&buf, binary.BigEndian, uint32(len(msg.Data))); err != nil {
		return nil, err
	}
	buf.Write(msg.Data)

	return buf.Bytes(), nil
}

// Decode decodes a binary message
func (c *BinaryMessageCodec) Decode(data []byte) (*BinaryPayload, error) {
	buf := bytes.NewReader(data)

	// Read type length
	var typeLen uint32
	if err := binary.Read(buf, binary.BigEndian, &typeLen); err != nil {
		return nil, err
	}

	// Validate type length
	if typeLen > 1024 { // Max type string length
		return nil, errors.New("invalid type length")
	}

	// Read type
	typeBytes := make([]byte, typeLen)
	if _, err := io.ReadFull(buf, typeBytes); err != nil {
		return nil, err
	}

	// Read data length
	var dataLen uint32
	if err := binary.Read(buf, binary.BigEndian, &dataLen); err != nil {
		return nil, err
	}

	// Validate data length
	if int64(dataLen) > c.maxMessageSize {
		return nil, fmt.Errorf("message size %d exceeds maximum %d", dataLen, c.maxMessageSize)
	}

	// Read data
	msgData := make([]byte, dataLen)
	if _, err := io.ReadFull(buf, msgData); err != nil {
		return nil, err
	}

	return &BinaryPayload{
		Type:      string(typeBytes),
		Data:      msgData,
		Timestamp: time.Now(),
	}, nil
}
