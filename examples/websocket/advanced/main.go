//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/goliatone/go-router"
)

// Global state management
var (
	// File transfer state
	fileStorage      = make(map[string]*FileData)
	fileStorageMutex sync.RWMutex

	// Collaboration state
	documentState = make(map[string]*Document)
	documentMutex sync.RWMutex

	// Gaming state
	gameRooms      = make(map[string]*GameRoom)
	gameRoomsMutex sync.RWMutex

	// Reconnection state
	sessions      = make(map[string]*ClientSession)
	sessionsMutex sync.RWMutex

	// Multi-protocol state
	protocolStats = make(map[string]int)
	protocolMutex sync.RWMutex

	// Collaboration client tracking
	documentClients = make(map[string]map[string]router.WebSocketContext) // docID -> clientID -> ws
	clientsMutex    sync.RWMutex
)

// Data structures for different features
type FileData struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Data     []byte    `json:"-"`
	Uploaded time.Time `json:"uploaded"`
}

type Document struct {
	ID      string                    `json:"id"`
	Content string                    `json:"content"`
	Version int                       `json:"version"`
	Cursors map[string]CursorPosition `json:"cursors"`
}

type CursorPosition struct {
	Line   int    `json:"line"`
	Column int    `json:"column"`
	User   string `json:"user"`
}

type GameRoom struct {
	ID      string             `json:"id"`
	Players map[string]*Player `json:"players"`
	State   string             `json:"state"`
}

type Player struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Position Position  `json:"position"`
	LastSeen time.Time `json:"lastSeen"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type ClientSession struct {
	ID         string           `json:"id"`
	ClientID   string           `json:"clientId"`
	Connected  bool             `json:"connected"`
	LastSeen   time.Time        `json:"lastSeen"`
	QueuedMsgs []*QueuedMessage `json:"queuedMessages"`
	Data       map[string]any   `json:"data"`
}

type QueuedMessage struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Data      any       `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

func main() {
	// Create HTTP server adapter
	app := router.NewHTTPServer()

	// Configure WebSocket settings
	config := router.DefaultWebSocketConfig()
	config.Origins = []string{"*"}
	config.ReadTimeout = 300 * time.Second
	config.PingPeriod = 30 * time.Second
	config.PongWait = 60 * time.Second
	config.MaxMessageSize = 10 * 1024 * 1024 // 10MB for file uploads

	// Setup all WebSocket endpoints
	setupFileTransferEndpoint(app.(*router.HTTPServer), config)
	setupCollaborationEndpoint(app.(*router.HTTPServer), config)
	setupGamingEndpoint(app.(*router.HTTPServer), config)
	setupReconnectionEndpoint(app.(*router.HTTPServer), config)
	setupMultiProtocolEndpoint(app.(*router.HTTPServer), config)

	// Setup HTTP routes
	app.Router().Get("/", dashboardHandler)
	app.Router().Get("/api/stats", statsHandler)

	// Health check endpoint
	app.Router().Get("/health", func(ctx router.Context) error {
		return ctx.JSON(200, map[string]any{
			"status": "healthy",
			"features": []string{
				"file_transfer", "collaboration", "gaming",
				"reconnection", "multi_protocol",
			},
			"timestamp": time.Now().Unix(),
		})
	})

	// Start server
	port := ":8080"
	log.Printf("🚀 Advanced WebSocket Features Dashboard starting on %s", port)
	log.Printf("📊 Dashboard: http://localhost%s/", port)
	log.Printf("🔧 Health: http://localhost%s/health", port)
	log.Printf("📈 Stats API: http://localhost%s/api/stats", port)

	if err := app.Serve(port); err != nil {
		log.Fatal("Server error:", err)
	}
}

// File Transfer WebSocket endpoint
func setupFileTransferEndpoint(app *router.HTTPServer, config router.WebSocketConfig) {
	fileHandler := func(ws router.WebSocketContext) error {
		log.Printf("[Files] Client connected: %s", ws.ConnectionID()[:8])

		// Send welcome and file list
		ws.WriteJSON(map[string]any{
			"type":    "welcome",
			"feature": "file_transfer",
			"message": "File Transfer System Ready",
			"files":   getFileList(),
		})

		for {
			var msg map[string]any
			if err := ws.ReadJSON(&msg); err != nil {
				log.Printf("[Files] Read error: %v", err)
				break
			}

			msgType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			switch msgType {
			case "upload":
				handleFileUpload(ws, msg)
			case "download":
				handleFileDownload(ws, msg)
			case "list":
				handleFileList(ws)
			case "delete":
				handleFileDelete(ws, msg)
			}
		}

		log.Printf("[Files] Client disconnected: %s", ws.ConnectionID()[:8])
		return nil
	}

	app.Router().WebSocket("/ws/files", config, fileHandler)
}

// Collaboration WebSocket endpoint
func setupCollaborationEndpoint(app *router.HTTPServer, config router.WebSocketConfig) {
	collabHandler := func(ws router.WebSocketContext) error {
		clientID := ws.ConnectionID()
		log.Printf("[Collab] Client connected: %s", clientID[:8])

		// Send welcome
		ws.WriteJSON(map[string]any{
			"type":      "welcome",
			"feature":   "collaboration",
			"message":   "Real-time Collaboration Ready",
			"documents": getDocumentList(),
		})

		for {
			var msg map[string]any
			if err := ws.ReadJSON(&msg); err != nil {
				log.Printf("[Collab] Read error: %v", err)
				break
			}

			msgType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			switch msgType {
			case "join_document":
				handleDocumentJoin(ws, msg)
			case "document_operation":
				handleDocumentOperation(ws, msg)
			case "cursor_position":
				handleCursorPosition(ws, msg)
			case "create_document":
				handleDocumentCreate(ws, msg)
			}
		}

		// Clean up client from all documents
		cleanupCollabClient(clientID)
		log.Printf("[Collab] Client disconnected: %s", clientID[:8])
		return nil
	}

	app.Router().WebSocket("/ws/collab", config, collabHandler)
}

// Gaming WebSocket endpoint
func setupGamingEndpoint(app *router.HTTPServer, config router.WebSocketConfig) {
	gameHandler := func(ws router.WebSocketContext) error {
		log.Printf("[Game] Player connected: %s", ws.ConnectionID()[:8])

		// Send welcome
		ws.WriteJSON(map[string]any{
			"type":    "welcome",
			"feature": "gaming",
			"message": "Gaming Server Ready",
			"rooms":   getGameRoomList(),
		})

		for {
			var msg map[string]any
			if err := ws.ReadJSON(&msg); err != nil {
				log.Printf("[Game] Read error: %v", err)
				break
			}

			msgType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			switch msgType {
			case "join_game":
				handleGameJoin(ws, msg)
			case "player_move":
				handlePlayerMove(ws, msg)
			case "game_action":
				handleGameAction(ws, msg)
			case "leave_game":
				handleGameLeave(ws, msg)
			case "pong":
				handlePong(ws)
			}
		}

		handleGameDisconnect(ws)
		log.Printf("[Game] Player disconnected: %s", ws.ConnectionID()[:8])
		return nil
	}

	app.Router().WebSocket("/ws/game", config, gameHandler)
}

// Reconnection WebSocket endpoint
func setupReconnectionEndpoint(app *router.HTTPServer, config router.WebSocketConfig) {
	reconnectHandler := func(ws router.WebSocketContext) error {
		clientID := ws.ConnectionID()
		log.Printf("[Reconnect] Client connected: %s", clientID[:8])

		var currentSessionID string

		// Check for reconnection attempt
		sessionID := getQueryParam(ws, "session_id")
		if sessionID != "" {
			log.Printf("[Reconnect] Attempting to reconnect with session: %s", sessionID)
			if handleReconnection(ws, sessionID) {
				log.Printf("[Reconnect] Successfully reconnected to session: %s", sessionID)
				currentSessionID = sessionID
			} else {
				log.Printf("[Reconnect] Failed to reconnect to session: %s (session not found or expired)", sessionID)
				// Send explicit message about failed reconnection
				ws.WriteJSON(map[string]any{
					"type":       "reconnection_failed",
					"session_id": sessionID,
					"message":    "Session not found or expired. Creating new session.",
				})
			}
		}

		// Create new session if reconnection failed or no session provided
		if currentSessionID == "" {
			session := createNewSession(clientID)
			currentSessionID = session.ID
			log.Printf("[Reconnect] Created new session: %s", currentSessionID)
			ws.WriteJSON(map[string]any{
				"type":       "session_created",
				"feature":    "reconnection",
				"session_id": session.ID,
				"message":    "New session created",
			})
		}

		for {
			var msg map[string]any
			if err := ws.ReadJSON(&msg); err != nil {
				log.Printf("[Reconnect] Read error: %v", err)
				break
			}

			msgType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			switch msgType {
			case "send_message":
				handleQueuedMessage(ws, msg)
			case "get_queued":
				handleGetQueuedMessages(ws, currentSessionID)
			}
		}

		handleSessionDisconnect(clientID)
		log.Printf("[Reconnect] Client disconnected: %s", clientID[:8])
		return nil
	}

	app.Router().WebSocket("/ws/reconnect", config, reconnectHandler)
}

// Multi-protocol WebSocket endpoint
func setupMultiProtocolEndpoint(app *router.HTTPServer, config router.WebSocketConfig) {
	multiHandler := func(ws router.WebSocketContext) error {
		protocol := getQueryParam(ws, "protocol")
		if protocol == "" {
			protocol = "json"
		}

		clientID := ws.ConnectionID()
		log.Printf("[Multi] Client connected with %s protocol: %s", protocol, clientID[:8])

		// Track protocol usage
		protocolMutex.Lock()
		protocolStats[protocol]++
		currentStats := getProtocolStatsUnsafe() // Get stats while holding lock
		protocolMutex.Unlock()

		log.Printf("[Multi] Protocol stats after connection: %+v", currentStats)

		// Send welcome based on protocol
		ws.WriteJSON(map[string]any{
			"type":     "welcome",
			"feature":  "multi_protocol",
			"protocol": protocol,
			"message":  fmt.Sprintf("Multi-Protocol Server Ready (%s)", protocol),
			"stats":    currentStats,
		})

		for {
			var msg map[string]any
			if err := ws.ReadJSON(&msg); err != nil {
				log.Printf("[Multi] Read error: %v", err)
				break
			}

			msgType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			switch msgType {
			case "ping":
				handleProtocolPing(ws, protocol)
			case "echo":
				handleProtocolEcho(ws, msg, protocol)
			case "stats":
				handleProtocolStats(ws)
			case "reset_stats":
				handleProtocolStatsReset(ws)
			}
		}

		// Decrement protocol count on disconnect
		protocolMutex.Lock()
		if protocolStats[protocol] > 0 {
			protocolStats[protocol]--
		}
		protocolMutex.Unlock()

		log.Printf("[Multi] Client disconnected (%s protocol): %s", protocol, clientID[:8])
		return nil
	}

	app.Router().WebSocket("/ws/multiproto", config, multiHandler)
}

// File transfer handlers
func handleFileUpload(ws router.WebSocketContext, msg map[string]any) {
	name, ok := msg["name"].(string)
	if !ok {
		ws.WriteJSON(map[string]any{"type": "error", "message": "Missing file name"})
		return
	}

	content, ok := msg["content"].(string)
	if !ok {
		ws.WriteJSON(map[string]any{"type": "error", "message": "Missing file content"})
		return
	}

	// Simulate file upload
	fileData := &FileData{
		ID:       generateID(),
		Name:     name,
		Size:     int64(len(content)),
		Data:     []byte(content),
		Uploaded: time.Now(),
	}

	fileStorageMutex.Lock()
	fileStorage[fileData.ID] = fileData
	fileStorageMutex.Unlock()

	ws.WriteJSON(map[string]any{
		"type": "upload_complete",
		"file": map[string]any{
			"id":   fileData.ID,
			"name": fileData.Name,
			"size": fileData.Size,
		},
		"message": fmt.Sprintf("File '%s' uploaded successfully", name),
	})
}

func handleFileDownload(ws router.WebSocketContext, msg map[string]any) {
	fileID, ok := msg["file_id"].(string)
	if !ok {
		ws.WriteJSON(map[string]any{"type": "error", "message": "Missing file ID"})
		return
	}

	fileStorageMutex.RLock()
	file, exists := fileStorage[fileID]
	fileStorageMutex.RUnlock()

	if !exists {
		ws.WriteJSON(map[string]any{"type": "error", "message": "File not found"})
		return
	}

	ws.WriteJSON(map[string]any{
		"type": "download_ready",
		"file": map[string]any{
			"id":      file.ID,
			"name":    file.Name,
			"size":    file.Size,
			"content": string(file.Data),
		},
		"message": fmt.Sprintf("File '%s' ready for download", file.Name),
	})
}

func handleFileList(ws router.WebSocketContext) {
	ws.WriteJSON(map[string]any{
		"type":  "file_list",
		"files": getFileList(),
	})
}

func handleFileDelete(ws router.WebSocketContext, msg map[string]any) {
	fileID, ok := msg["file_id"].(string)
	if !ok {
		ws.WriteJSON(map[string]any{"type": "error", "message": "Missing file ID"})
		return
	}

	fileStorageMutex.Lock()
	file, exists := fileStorage[fileID]
	if exists {
		delete(fileStorage, fileID)
	}
	fileStorageMutex.Unlock()

	if !exists {
		ws.WriteJSON(map[string]any{"type": "error", "message": "File not found"})
		return
	}

	ws.WriteJSON(map[string]any{
		"type":    "file_deleted",
		"message": fmt.Sprintf("File '%s' deleted successfully", file.Name),
	})
}

// Collaboration handlers
func handleDocumentJoin(ws router.WebSocketContext, msg map[string]any) {
	docID, ok := msg["document_id"].(string)
	if !ok {
		ws.WriteJSON(map[string]any{"type": "error", "message": "Missing document ID"})
		return
	}

	clientID := ws.ConnectionID()

	documentMutex.Lock()
	doc, exists := documentState[docID]
	if !exists {
		doc = &Document{
			ID:      docID,
			Content: "# Welcome to Document Collaboration\n\nStart typing to collaborate in real-time!",
			Version: 1,
			Cursors: make(map[string]CursorPosition),
		}
		documentState[docID] = doc
	}
	documentMutex.Unlock()

	// Track this client as viewing this document
	clientsMutex.Lock()
	if documentClients[docID] == nil {
		documentClients[docID] = make(map[string]router.WebSocketContext)
	}
	documentClients[docID][clientID] = ws
	clientsMutex.Unlock()

	ws.WriteJSON(map[string]any{
		"type":           "document_joined",
		"document":       doc,
		"message":        fmt.Sprintf("Joined document '%s'", docID),
		"active_clients": len(documentClients[docID]),
	})

	// Notify other clients that someone joined
	broadcastToDocument(docID, map[string]any{
		"type":           "client_joined",
		"client_id":      clientID[:8],
		"active_clients": len(documentClients[docID]),
	}, clientID)
}

func handleDocumentOperation(ws router.WebSocketContext, msg map[string]any) {
	docID, ok := msg["document_id"].(string)
	if !ok {
		return
	}

	operation, ok := msg["operation"].(string)
	if !ok {
		return
	}

	clientID := ws.ConnectionID()

	documentMutex.Lock()
	doc, exists := documentState[docID]
	if exists {
		// Update document
		doc.Version++
		if content, ok := msg["content"].(string); ok {
			doc.Content = content
		}
	}
	documentMutex.Unlock()

	if exists {
		// Broadcast to ALL clients viewing this document
		broadcastData := map[string]any{
			"type":      "document_updated",
			"document":  doc,
			"operation": operation,
			"editor_id": clientID[:8],
		}

		// Send confirmation to sender
		ws.WriteJSON(broadcastData)

		// Broadcast to other clients
		broadcastToDocument(docID, broadcastData, clientID)
	}
}

func handleCursorPosition(ws router.WebSocketContext, msg map[string]any) {
	docID, ok := msg["document_id"].(string)
	if !ok {
		return
	}

	userID := ws.ConnectionID()[:8]
	line, _ := msg["line"].(float64)
	column, _ := msg["column"].(float64)

	documentMutex.Lock()
	doc, exists := documentState[docID]
	if exists {
		doc.Cursors[userID] = CursorPosition{
			Line:   int(line),
			Column: int(column),
			User:   userID,
		}
	}
	documentMutex.Unlock()

	if exists {
		ws.WriteJSON(map[string]any{
			"type":    "cursor_updated",
			"cursors": doc.Cursors,
		})
	}
}

func handleDocumentCreate(ws router.WebSocketContext, msg map[string]any) {
	name, ok := msg["name"].(string)
	if !ok {
		ws.WriteJSON(map[string]any{"type": "error", "message": "Missing document name"})
		return
	}

	doc := &Document{
		ID:      generateID(),
		Content: fmt.Sprintf("# %s\n\nNew document created at %s", name, time.Now().Format("2006-01-02 15:04:05")),
		Version: 1,
		Cursors: make(map[string]CursorPosition),
	}

	documentMutex.Lock()
	documentState[doc.ID] = doc
	documentMutex.Unlock()

	ws.WriteJSON(map[string]any{
		"type":     "document_created",
		"document": doc,
		"message":  fmt.Sprintf("Document '%s' created successfully", name),
	})
}

// Broadcast a message to all clients viewing a specific document (except sender)
func broadcastToDocument(docID string, message map[string]any, excludeClientID string) {
	clientsMutex.RLock()
	clients, exists := documentClients[docID]
	if !exists {
		clientsMutex.RUnlock()
		return
	}

	// Create a copy of the clients map to avoid holding the lock while sending
	clientsCopy := make(map[string]router.WebSocketContext)
	for id, ws := range clients {
		if id != excludeClientID {
			clientsCopy[id] = ws
		}
	}
	clientsMutex.RUnlock()

	// Send message to all clients
	for clientID, ws := range clientsCopy {
		if err := ws.WriteJSON(message); err != nil {
			// Remove disconnected client
			removeClientFromDocument(docID, clientID)
		}
	}
}

// Remove a client from all document tracking
func removeClientFromDocument(docID, clientID string) {
	clientsMutex.Lock()
	defer clientsMutex.Unlock()

	if clients, exists := documentClients[docID]; exists {
		delete(clients, clientID)
		// If no more clients, clean up the document entry
		if len(clients) == 0 {
			delete(documentClients, docID)
		}
	}
}

// Clean up a client from all documents when they disconnect
func cleanupCollabClient(clientID string) {
	clientsMutex.Lock()
	defer clientsMutex.Unlock()

	for docID, clients := range documentClients {
		if _, exists := clients[clientID]; exists {
			delete(clients, clientID)
			// Notify remaining clients
			if len(clients) > 0 {
				go broadcastToDocument(docID, map[string]any{
					"type":           "client_left",
					"client_id":      clientID[:8],
					"active_clients": len(clients),
				}, "")
			}
			// Clean up empty document entries
			if len(clients) == 0 {
				delete(documentClients, docID)
			}
		}
	}
}

// Gaming handlers
func handleGameJoin(ws router.WebSocketContext, msg map[string]any) {
	gameID, ok := msg["game_id"].(string)
	if !ok {
		gameID = "lobby"
	}

	playerName, ok := msg["player_name"].(string)
	if !ok {
		playerName = "Player" + ws.ConnectionID()[:6]
	}

	gameRoomsMutex.Lock()
	room, exists := gameRooms[gameID]
	if !exists {
		room = &GameRoom{
			ID:      gameID,
			Players: make(map[string]*Player),
			State:   "waiting",
		}
		gameRooms[gameID] = room
	}

	player := &Player{
		ID:       ws.ConnectionID(),
		Name:     playerName,
		Position: Position{X: 0, Y: 0},
		LastSeen: time.Now(),
	}
	room.Players[ws.ConnectionID()] = player
	gameRoomsMutex.Unlock()

	ws.WriteJSON(map[string]any{
		"type":    "game_joined",
		"game_id": gameID,
		"player":  player,
		"room":    room,
		"message": fmt.Sprintf("Joined game '%s' as '%s'", gameID, playerName),
	})
}

func handlePlayerMove(ws router.WebSocketContext, msg map[string]any) {
	gameID, ok := msg["game_id"].(string)
	if !ok {
		return
	}

	x, _ := msg["x"].(float64)
	y, _ := msg["y"].(float64)

	gameRoomsMutex.Lock()
	room, exists := gameRooms[gameID]
	if exists {
		player, playerExists := room.Players[ws.ConnectionID()]
		if playerExists {
			player.Position.X = x
			player.Position.Y = y
			player.LastSeen = time.Now()
		}
	}
	gameRoomsMutex.Unlock()

	if exists {
		ws.WriteJSON(map[string]any{
			"type":      "player_moved",
			"player_id": ws.ConnectionID(),
			"position":  map[string]float64{"x": x, "y": y},
			"timestamp": time.Now().Unix(),
		})
	}
}

func handleGameAction(ws router.WebSocketContext, msg map[string]any) {
	gameID, ok := msg["game_id"].(string)
	if !ok {
		return
	}

	action, ok := msg["action"].(string)
	if !ok {
		return
	}

	ws.WriteJSON(map[string]any{
		"type":      "game_action_result",
		"game_id":   gameID,
		"action":    action,
		"player_id": ws.ConnectionID(),
		"timestamp": time.Now().Unix(),
		"result":    "Action processed successfully",
	})
}

func handleGameLeave(ws router.WebSocketContext, _ map[string]any) {
	handleGameDisconnect(ws)
}

func handleGameDisconnect(ws router.WebSocketContext) {
	gameRoomsMutex.Lock()
	defer gameRoomsMutex.Unlock()

	for gameID, room := range gameRooms {
		if player, exists := room.Players[ws.ConnectionID()]; exists {
			delete(room.Players, ws.ConnectionID())
			log.Printf("[Game] Player %s left game %s", player.Name, gameID)

			// Clean up empty rooms
			if len(room.Players) == 0 {
				delete(gameRooms, gameID)
			}
			break
		}
	}
}

func handlePong(ws router.WebSocketContext) {
	ws.WriteJSON(map[string]any{
		"type":      "ping",
		"timestamp": time.Now().Unix(),
	})
}

// Reconnection handlers
func handleReconnection(ws router.WebSocketContext, sessionID string) bool {
	sessionsMutex.Lock()
	session, exists := sessions[sessionID]
	if exists {
		session.Connected = true
		session.ClientID = ws.ConnectionID()
		session.LastSeen = time.Now()

		// Send queued messages
		for _, msg := range session.QueuedMsgs {
			ws.WriteJSON(map[string]any{
				"type":          "queued_message",
				"original_type": msg.Type,
				"data":          msg.Data,
				"queued_at":     msg.Timestamp,
			})
		}
		session.QueuedMsgs = nil // Clear queue
	}
	sessionsMutex.Unlock()

	if exists {
		ws.WriteJSON(map[string]any{
			"type":       "reconnected",
			"session_id": sessionID,
			"message":    "Successfully reconnected to existing session",
		})
		return true
	}
	return false
}

func createNewSession(clientID string) *ClientSession {
	session := &ClientSession{
		ID:         generateID(),
		ClientID:   clientID,
		Connected:  true,
		LastSeen:   time.Now(),
		QueuedMsgs: make([]*QueuedMessage, 0),
		Data:       make(map[string]any),
	}

	sessionsMutex.Lock()
	sessions[session.ID] = session
	sessionsMutex.Unlock()

	return session
}

func handleQueuedMessage(ws router.WebSocketContext, msg map[string]any) {
	targetSession, ok := msg["target_session"].(string)
	if !ok {
		ws.WriteJSON(map[string]any{"type": "error", "message": "Missing target session"})
		return
	}

	messageData, ok := msg["data"]
	if !ok {
		ws.WriteJSON(map[string]any{"type": "error", "message": "Missing message data"})
		return
	}

	queuedMsg := &QueuedMessage{
		ID:        generateID(),
		Type:      "user_message",
		Data:      messageData,
		Timestamp: time.Now(),
	}

	sessionsMutex.Lock()
	session, exists := sessions[targetSession]
	if exists {
		session.QueuedMsgs = append(session.QueuedMsgs, queuedMsg)
	}
	sessionsMutex.Unlock()

	if exists {
		ws.WriteJSON(map[string]any{
			"type":    "message_queued",
			"message": "Message queued for offline user",
		})
	} else {
		ws.WriteJSON(map[string]any{"type": "error", "message": "Target session not found"})
	}
}

func handleGetQueuedMessages(ws router.WebSocketContext, sessionID string) {
	log.Printf("[Reconnect] Getting queued messages for session: %s", sessionID)

	sessionsMutex.RLock()
	session, exists := sessions[sessionID]
	var msgs []*QueuedMessage
	if exists {
		msgs = make([]*QueuedMessage, len(session.QueuedMsgs))
		copy(msgs, session.QueuedMsgs)
		log.Printf("[Reconnect] Found %d queued messages for session %s", len(msgs), sessionID)
	} else {
		log.Printf("[Reconnect] Session %s not found", sessionID)
	}
	sessionsMutex.RUnlock()

	if exists {
		ws.WriteJSON(map[string]any{
			"type":     "queued_messages",
			"messages": msgs,
			"count":    len(msgs),
		})
	} else {
		ws.WriteJSON(map[string]any{
			"type":    "error",
			"message": "Session not found: " + sessionID,
		})
	}
}

func handleSessionDisconnect(clientID string) {
	sessionsMutex.Lock()
	for _, session := range sessions {
		if session.ClientID == clientID {
			session.Connected = false
			session.LastSeen = time.Now()
			break
		}
	}
	sessionsMutex.Unlock()
}

// Multi-protocol handlers
func handleProtocolPing(ws router.WebSocketContext, protocol string) {
	ws.WriteJSON(map[string]any{
		"type":      "pong",
		"protocol":  protocol,
		"timestamp": time.Now().Unix(),
	})
}

func handleProtocolEcho(ws router.WebSocketContext, msg map[string]any, protocol string) {
	ws.WriteJSON(map[string]any{
		"type":          "echo_response",
		"protocol":      protocol,
		"original_data": msg["data"],
		"timestamp":     time.Now().Unix(),
	})
}

func handleProtocolStats(ws router.WebSocketContext) {
	ws.WriteJSON(map[string]any{
		"type":  "protocol_stats",
		"stats": getProtocolStats(),
	})
}

func handleProtocolStatsReset(ws router.WebSocketContext) {
	log.Printf("[Multi] Resetting protocol stats")
	protocolMutex.Lock()
	protocolStats = make(map[string]int)
	protocolMutex.Unlock()

	ws.WriteJSON(map[string]any{
		"type":    "stats_reset",
		"message": "Protocol statistics reset",
		"stats":   getProtocolStats(),
	})
}

// Helper functions
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func getQueryParam(ws router.WebSocketContext, key string) string {
	// Get query parameters from the WebSocket request
	return ws.Query(key)
}

func getFileList() []map[string]any {
	fileStorageMutex.RLock()
	defer fileStorageMutex.RUnlock()

	files := make([]map[string]any, 0, len(fileStorage))
	for _, file := range fileStorage {
		files = append(files, map[string]any{
			"id":       file.ID,
			"name":     file.Name,
			"size":     file.Size,
			"uploaded": file.Uploaded.Format("2006-01-02 15:04:05"),
		})
	}
	return files
}

func getDocumentList() []map[string]any {
	documentMutex.RLock()
	defer documentMutex.RUnlock()

	docs := make([]map[string]any, 0, len(documentState))
	for _, doc := range documentState {
		docs = append(docs, map[string]any{
			"id":      doc.ID,
			"version": doc.Version,
			"cursors": len(doc.Cursors),
		})
	}
	return docs
}

func getGameRoomList() []map[string]any {
	gameRoomsMutex.RLock()
	defer gameRoomsMutex.RUnlock()

	rooms := make([]map[string]any, 0, len(gameRooms))
	for _, room := range gameRooms {
		rooms = append(rooms, map[string]any{
			"id":      room.ID,
			"players": len(room.Players),
			"state":   room.State,
		})
	}
	return rooms
}

func getProtocolStats() map[string]int {
	protocolMutex.RLock()
	defer protocolMutex.RUnlock()
	return getProtocolStatsUnsafe()
}

func getProtocolStatsUnsafe() map[string]int {
	stats := make(map[string]int)
	for protocol, count := range protocolStats {
		stats[protocol] = count
	}
	return stats
}

// HTTP handlers
func dashboardHandler(ctx router.Context) error {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>🚀 Advanced WebSocket Features Dashboard</title>
    <meta charset="utf-8">
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            padding: 20px;
        }
        .container { max-width: 1400px; margin: 0 auto; }
        .header {
            text-align: center;
            color: white;
            margin-bottom: 30px;
            padding: 20px;
            background: rgba(255,255,255,0.1);
            border-radius: 15px;
            backdrop-filter: blur(10px);
        }
        .header h1 { font-size: 2.5em; margin-bottom: 10px; }
        .header p { font-size: 1.2em; opacity: 0.9; }

        .tabs {
            display: flex;
            background: rgba(255,255,255,0.2);
            border-radius: 15px 15px 0 0;
            overflow: hidden;
            margin-bottom: 0;
        }
        .tab {
            flex: 1;
            padding: 15px 20px;
            cursor: pointer;
            background: transparent;
            color: white;
            border: none;
            font-size: 16px;
            transition: background 0.3s;
        }
        .tab:hover { background: rgba(255,255,255,0.1); }
        .tab.active {
            background: rgba(255,255,255,0.3);
            font-weight: bold;
        }

        .content {
            background: white;
            border-radius: 0 0 15px 15px;
            min-height: 600px;
            box-shadow: 0 20px 40px rgba(0,0,0,0.1);
            overflow: visible;
            transition: all 0.3s ease;
        }
        .feature {
            display: none;
            padding: 30px;
            min-height: 540px; /* 600px - 60px padding */
        }
        .feature.active { display: block; }
        .feature h2 {
            color: #333;
            margin-bottom: 20px;
            font-size: 1.8em;
        }

        .controls {
            background: #f8f9fa;
            padding: 20px;
            border-radius: 10px;
            margin-bottom: 20px;
            display: flex;
            gap: 10px;
            flex-wrap: wrap;
            align-items: center;
        }
        .status {
            padding: 8px 16px;
            border-radius: 20px;
            font-weight: bold;
            font-size: 14px;
        }
        .status.connected { background: #d4edda; color: #155724; }
        .status.disconnected { background: #f8d7da; color: #721c24; }

        button {
            padding: 10px 20px;
            border: none;
            border-radius: 8px;
            cursor: pointer;
            font-size: 14px;
            font-weight: 500;
            transition: all 0.3s;
        }
        .btn-primary { background: #007bff; color: white; }
        .btn-primary:hover { background: #0056b3; transform: translateY(-2px); }
        .btn-success { background: #28a745; color: white; }
        .btn-success:hover { background: #1e7e34; }
        .btn-danger { background: #dc3545; color: white; }
        .btn-danger:hover { background: #c82333; }
        .btn-secondary { background: #6c757d; color: white; }
        .btn-secondary:hover { background: #545b62; }

        input, textarea, select {
            padding: 10px;
            border: 2px solid #e1e1e1;
            border-radius: 8px;
            font-size: 14px;
            transition: border-color 0.3s;
        }
        input:focus, textarea:focus, select:focus {
            outline: none;
            border-color: #007bff;
        }

        .messages {
            height: 300px;
            overflow-y: auto;
            border: 2px solid #e1e1e1;
            border-radius: 8px;
            padding: 15px;
            background: #fafafa;
            margin: 15px 0;
        }
        .message {
            margin: 8px 0;
            padding: 8px 12px;
            border-radius: 6px;
            font-size: 14px;
            line-height: 1.4;
        }
        .message.info { background: #e3f2fd; border-left: 4px solid #2196f3; }
        .message.success { background: #e8f5e8; border-left: 4px solid #4caf50; }
        .message.error { background: #ffebee; border-left: 4px solid #f44336; }
        .message.warning { background: #fff8e1; border-left: 4px solid #ff9800; }

        .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 20px; }
        .card {
            background: #f8f9fa;
            padding: 20px;
            border-radius: 10px;
            border: 1px solid #e1e1e1;
        }
        .card h3 { color: #495057; margin-bottom: 15px; }

        .stats {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
            gap: 15px;
            margin: 20px 0;
        }
        .stat {
            text-align: center;
            padding: 20px;
            background: linear-gradient(45deg, #007bff, #0056b3);
            color: white;
            border-radius: 10px;
        }
        .stat .number { font-size: 2em; font-weight: bold; }
        .stat .label { font-size: 0.9em; opacity: 0.9; }

        .file-list, .doc-list, .room-list {
            max-height: 200px;
            overflow-y: auto;
        }
        .list-item {
            padding: 10px;
            border-bottom: 1px solid #e1e1e1;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        .list-item:hover { background: #f8f9fa; }

        .canvas {
            border: 2px solid #e1e1e1;
            border-radius: 8px;
            cursor: crosshair;
        }

        @media (max-width: 768px) {
            .grid { grid-template-columns: 1fr; }
            .tabs { flex-wrap: wrap; }
            .tab { min-width: 120px; }
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🚀 Advanced WebSocket Features</h1>
            <p>Interactive dashboard showcasing file transfer, collaboration, gaming, reconnection, and multi-protocol features</p>
        </div>

        <div class="tabs">
            <button class="tab active" onclick="showFeature('files')">📁 File Transfer</button>
            <button class="tab" onclick="showFeature('collab')">📝 Collaboration</button>
            <button class="tab" onclick="showFeature('game')">🎮 Gaming</button>
            <button class="tab" onclick="showFeature('reconnect')">🔄 Reconnection</button>
            <button class="tab" onclick="showFeature('multiproto')">🌐 Multi-Protocol</button>
        </div>

        <div class="content">
            <!-- File Transfer Feature -->
            <div id="files" class="feature active">
                <h2>📁 File Transfer System</h2>
                <div class="controls">
                    <span id="files-status" class="status disconnected">Disconnected</span>
                    <button onclick="connectFiles()" class="btn-primary">Connect</button>
                    <button onclick="disconnectFiles()" class="btn-secondary">Disconnect</button>
                    <input type="file" id="file-input" style="margin-left: 20px;">
                    <button onclick="uploadFile()" class="btn-success">Upload File</button>
                </div>

                <div class="grid">
                    <div class="card">
                        <h3>File Operations</h3>
                        <div id="files-messages" class="messages"></div>
                        <div style="margin-top: 15px;">
                            <button onclick="listFiles()" class="btn-secondary">List Files</button>
                        </div>
                    </div>

                    <div class="card">
                        <h3>Available Files</h3>
                        <div id="file-list" class="file-list"></div>
                    </div>
                </div>
            </div>

            <!-- Collaboration Feature -->
            <div id="collab" class="feature">
                <h2>📝 Real-time Collaboration</h2>
                <div class="controls">
                    <span id="collab-status" class="status disconnected">Disconnected</span>
                    <button onclick="connectCollab()" class="btn-primary">Connect</button>
                    <button onclick="disconnectCollab()" class="btn-secondary">Disconnect</button>
                    <input type="text" id="doc-name" placeholder="New document name" style="margin-left: 20px;">
                    <button onclick="createDocument()" class="btn-success">Create Document</button>
                </div>

                <div class="grid">
                    <div class="card">
                        <h3>Document Editor</h3>
                        <select id="doc-select" onchange="joinDocument()">
                            <option value="">Select a document...</option>
                        </select>
                        <textarea id="doc-content" placeholder="Document content..." style="width: 100%; height: 200px; margin-top: 10px;"
                                  oninput="scheduleAutoSave()"></textarea>
                        <button onclick="saveDocument()" class="btn-success" style="margin-top: 10px;">Save Changes</button>
                        <span id="save-status" style="margin-left: 10px; color: #666;"></span>
                    </div>

                    <div class="card">
                        <h3>Collaboration Activity</h3>
                        <div id="collab-messages" class="messages"></div>
                    </div>
                </div>
            </div>

            <!-- Gaming Feature -->
            <div id="game" class="feature">
                <h2>🎮 Gaming Server</h2>
                <div class="controls">
                    <span id="game-status" class="status disconnected">Disconnected</span>
                    <button onclick="connectGame()" class="btn-primary">Connect</button>
                    <button onclick="disconnectGame()" class="btn-secondary">Disconnect</button>
                    <input type="text" id="player-name" placeholder="Player name" value="Player1" style="margin-left: 20px;">
                    <input type="text" id="game-id" placeholder="Game ID" value="lobby">
                    <button onclick="joinGame()" class="btn-success">Join Game</button>
                </div>

                <div class="grid">
                    <div class="card">
                        <h3>Game Canvas</h3>
                        <canvas id="game-canvas" class="canvas" width="400" height="300" onclick="movePlayer(event)"></canvas>
                        <div style="margin-top: 10px;">
                            <button onclick="sendGameAction('attack')" class="btn-danger">Attack</button>
                            <button onclick="sendGameAction('defend')" class="btn-secondary">Defend</button>
                            <button onclick="sendGameAction('special')" class="btn-primary">Special</button>
                        </div>
                    </div>

                    <div class="card">
                        <h3>Game Activity</h3>
                        <div id="game-messages" class="messages"></div>
                        <div class="stats" style="grid-template-columns: 1fr 1fr;">
                            <div class="stat">
                                <div class="number" id="player-count">0</div>
                                <div class="label">Players</div>
                            </div>
                            <div class="stat">
                                <div class="number" id="room-count">0</div>
                                <div class="label">Rooms</div>
                            </div>
                        </div>
                    </div>
                </div>
            </div>

            <!-- Reconnection Feature -->
            <div id="reconnect" class="feature">
                <h2>🔄 Reconnection & Message Queueing</h2>

                <div style="background: #e3f2fd; padding: 15px; border-radius: 8px; margin-bottom: 20px; border-left: 4px solid #2196f3;">
                    <h4 style="margin: 0 0 10px 0; color: #1565c0;">💡 How to test reconnection:</h4>
                    <ol style="margin: 0; padding-left: 20px; color: #1976d2;">
                        <li><strong>Connect</strong> to get a session ID</li>
                        <li><strong>Send yourself messages</strong> using your session ID</li>
                        <li><strong>Disconnect</strong> to simulate connection loss</li>
                        <li><strong>Reconnect</strong> to receive queued messages</li>
                        <li>Or <strong>test with multiple tabs</strong>: use one session ID in both tabs</li>
                    </ol>
                </div>

                <div class="controls">
                    <span id="reconnect-status" class="status disconnected">Disconnected</span>
                    <button onclick="connectReconnect()" class="btn-primary">Connect</button>
                    <button onclick="disconnectReconnect()" class="btn-secondary">Disconnect</button>
                    <button onclick="simulateReconnect()" class="btn-success" style="margin-left: 20px;">Simulate Reconnect</button>
                    <span id="session-id" style="margin-left: 20px; font-family: monospace; background: #f8f9fa; padding: 5px 10px; border-radius: 4px;"></span>
                </div>

                <div class="grid">
                    <div class="card">
                        <h3>Message Queue Testing</h3>
                        <div style="margin-bottom: 15px;">
                            <label style="display: block; margin-bottom: 5px; font-weight: bold;">Message to queue:</label>
                            <input type="text" id="queue-message" placeholder="Type your message here..." style="width: 100%; margin-bottom: 10px;">
                        </div>
                        <div style="margin-bottom: 15px;">
                            <label style="display: block; margin-bottom: 5px; font-weight: bold;">Target session ID:</label>
                            <input type="text" id="target-session" placeholder="Paste session ID or use your own" style="width: 100%; margin-bottom: 10px;">
                            <button onclick="useMySession()" class="btn-secondary" style="padding: 5px 10px; font-size: 12px;">Use My Session</button>
                        </div>
                        <div>
                            <button onclick="sendQueuedMessage()" class="btn-success">Queue Message</button>
                            <button onclick="getQueuedMessages()" class="btn-secondary" style="margin-left: 10px;">Get Queued Messages</button>
                            <button onclick="quickTest()" class="btn-primary" style="margin-left: 10px; padding: 5px 10px; font-size: 12px;">Quick Test</button>
                        </div>
                    </div>

                    <div class="card">
                        <h3>Reconnection Activity</h3>
                        <div id="reconnect-messages" class="messages"></div>
                        <div style="margin-top: 15px;">
                            <button onclick="clearReconnectMessages()" class="btn-secondary" style="padding: 5px 10px; font-size: 12px;">Clear Messages</button>
                            <button onclick="checkConnectionStatus()" class="btn-secondary" style="padding: 5px 10px; font-size: 12px; margin-left: 5px;">Check Status</button>
                            <button onclick="clearStoredSession()" class="btn-danger" style="padding: 5px 10px; font-size: 12px; margin-left: 5px;">Clear Stored Session</button>
                        </div>
                    </div>
                </div>
            </div>

            <!-- Multi-Protocol Feature -->
            <div id="multiproto" class="feature">
                <h2>🌐 Multi-Protocol Support</h2>
                <div class="controls">
                    <span id="multiproto-status" class="status disconnected">Disconnected</span>
                    <select id="protocol-select" onchange="protocolChanged()">
                        <option value="json">JSON Protocol</option>
                        <option value="binary">Binary Protocol</option>
                        <option value="msgpack">MessagePack Protocol</option>
                    </select>
                    <button onclick="connectMultiProto()" class="btn-primary">Connect</button>
                    <button onclick="disconnectMultiProto()" class="btn-secondary">Disconnect</button>
                    <button onclick="sendProtocolPing()" class="btn-success" style="margin-left: 20px;">Send Ping</button>
                </div>

                <div class="grid">
                    <div class="card">
                        <h3>Protocol Testing</h3>
                        <input type="text" id="echo-message" placeholder="Message to echo..." style="width: 100%; margin-bottom: 10px;">
                        <button onclick="sendEcho()" class="btn-success">Send Echo</button>
                        <button onclick="getProtocolStats()" class="btn-secondary" style="margin-left: 10px;">Get Stats</button>
                        <button onclick="resetProtocolStats()" class="btn-danger" style="padding: 5px 10px; font-size: 12px; margin-left: 5px;">Reset Stats</button>
                        <div class="stats" style="margin-top: 20px;">
                            <div class="stat">
                                <div class="number" id="json-count">0</div>
                                <div class="label">JSON</div>
                            </div>
                            <div class="stat">
                                <div class="number" id="binary-count">0</div>
                                <div class="label">Binary</div>
                            </div>
                            <div class="stat">
                                <div class="number" id="msgpack-count">0</div>
                                <div class="label">MsgPack</div>
                            </div>
                        </div>
                    </div>

                    <div class="card">
                        <h3>Protocol Activity</h3>
                        <div id="multiproto-messages" class="messages"></div>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <script>
        // Global WebSocket connections
        let filesWs = null;
        let collabWs = null;
        let gameWs = null;
        let reconnectWs = null;
        let multiprotoWs = null;
        let currentSessionId = null;

        // Utility functions
        function showFeature(feature) {
            document.querySelectorAll('.feature').forEach(f => f.classList.remove('active'));
            document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
            document.getElementById(feature).classList.add('active');
            event.target.classList.add('active');
        }

        function addMessage(containerId, message, type = 'info') {
            const container = document.getElementById(containerId);
            const div = document.createElement('div');
            div.className = 'message ' + type;
            div.innerHTML = new Date().toLocaleTimeString() + ' - ' + message;
            container.appendChild(div);
            container.scrollTop = container.scrollHeight;
        }

        function updateStatus(statusId, connected) {
            const status = document.getElementById(statusId);
            status.textContent = connected ? 'Connected' : 'Disconnected';
            status.className = 'status ' + (connected ? 'connected' : 'disconnected');
        }

        // File Transfer Functions
        function connectFiles() {
            if (filesWs) return;
            filesWs = new WebSocket('ws://localhost:8080/ws/files');

            filesWs.onopen = () => {
                updateStatus('files-status', true);
                addMessage('files-messages', '✅ Connected to File Transfer System', 'success');
            };

            filesWs.onmessage = (e) => {
                const data = JSON.parse(e.data);
                handleFilesMessage(data);
            };

            filesWs.onclose = () => {
                updateStatus('files-status', false);
                addMessage('files-messages', '❌ Disconnected from File Transfer System', 'error');
                filesWs = null;
            };
        }

        function disconnectFiles() {
            if (filesWs) {
                filesWs.close();
            }
        }

        function uploadFile() {
            if (!filesWs) {
                addMessage('files-messages', '❌ Not connected', 'error');
                return;
            }

            const fileInput = document.getElementById('file-input');
            const file = fileInput.files[0];
            if (!file) {
                addMessage('files-messages', '❌ Please select a file', 'error');
                return;
            }

            // Check file size (max 8MB to account for JSON encoding overhead)
            const maxSize = 8 * 1024 * 1024;
            if (file.size > maxSize) {
                addMessage('files-messages', '❌ File too large (max 8MB)', 'error');
                return;
            }

            addMessage('files-messages', '📤 Uploading ' + file.name + ' (' + file.size + ' bytes)...', 'info');

            const reader = new FileReader();
            reader.onload = function(e) {
                try {
                    filesWs.send(JSON.stringify({
                        type: 'upload',
                        name: file.name,
                        content: e.target.result
                    }));
                } catch (error) {
                    addMessage('files-messages', '❌ Upload failed: ' + error.message, 'error');
                }
            };
            reader.onerror = function() {
                addMessage('files-messages', '❌ Failed to read file', 'error');
            };
            reader.readAsText(file);
        }

        function listFiles() {
            if (!filesWs) return;
            filesWs.send(JSON.stringify({ type: 'list' }));
        }

        function downloadFile(fileId) {
            if (!filesWs) return;
            filesWs.send(JSON.stringify({ type: 'download', file_id: fileId }));
        }

        function deleteFile(fileId) {
            if (!filesWs) return;
            filesWs.send(JSON.stringify({ type: 'delete', file_id: fileId }));
        }

        function handleFilesMessage(data) {
            switch (data.type) {
                case 'welcome':
                    addMessage('files-messages', data.message, 'success');
                    updateFileList(data.files);
                    break;
                case 'upload_complete':
                    addMessage('files-messages', data.message, 'success');
                    listFiles();
                    break;
                case 'download_ready':
                    addMessage('files-messages', data.message, 'success');
                    // Could trigger actual download here
                    break;
                case 'file_list':
                    updateFileList(data.files);
                    break;
                case 'file_deleted':
                    addMessage('files-messages', data.message, 'success');
                    listFiles();
                    break;
                case 'error':
                    addMessage('files-messages', data.message, 'error');
                    break;
                default:
                    addMessage('files-messages', JSON.stringify(data), 'info');
            }
        }

        function updateFileList(files) {
            const container = document.getElementById('file-list');
            if (!files || files.length === 0) {
                container.innerHTML = '<div style="padding: 20px; text-align: center; color: #666;">No files uploaded</div>';
                return;
            }

            container.innerHTML = files.map(file =>
                '<div class="list-item">' +
                    '<div><strong>' + file.name + '</strong><br><small>' + file.size + ' bytes - ' + file.uploaded + '</small></div>' +
                    '<div>' +
                        '<button onclick="downloadFile(\'' + file.id + '\')" class="btn-primary" style="margin-right: 5px; padding: 5px 10px;">Download</button>' +
                        '<button onclick="deleteFile(\'' + file.id + '\')" class="btn-danger" style="padding: 5px 10px;">Delete</button>' +
                    '</div>' +
                '</div>'
            ).join('');
        }

        // Collaboration Functions
        let autoSaveTimeout = null;
        let isAutoSaving = false;

        function connectCollab() {
            if (collabWs) return;
            collabWs = new WebSocket('ws://localhost:8080/ws/collab');

            collabWs.onopen = () => {
                updateStatus('collab-status', true);
                addMessage('collab-messages', '✅ Connected to Collaboration System', 'success');
            };

            collabWs.onmessage = (e) => {
                const data = JSON.parse(e.data);
                handleCollabMessage(data);
            };

            collabWs.onclose = () => {
                updateStatus('collab-status', false);
                addMessage('collab-messages', '❌ Disconnected from Collaboration System', 'error');
                collabWs = null;
            };
        }

        function scheduleAutoSave() {
            // Don't auto-save if we're in the middle of updating from another client
            if (isAutoSaving) return;

            // Clear existing timeout
            if (autoSaveTimeout) {
                clearTimeout(autoSaveTimeout);
            }

            // Update status
            document.getElementById('save-status').textContent = 'Typing...';

            // Schedule auto-save after 1 second of no typing
            autoSaveTimeout = setTimeout(() => {
                saveDocument(true); // true indicates auto-save
            }, 1000);
        }

        function disconnectCollab() {
            if (collabWs) {
                collabWs.close();
            }
        }

        function createDocument() {
            if (!collabWs) return;
            const name = document.getElementById('doc-name').value;
            if (!name) {
                addMessage('collab-messages', '❌ Please enter a document name', 'error');
                return;
            }
            collabWs.send(JSON.stringify({ type: 'create_document', name: name }));
            document.getElementById('doc-name').value = '';
        }

        function joinDocument() {
            if (!collabWs) return;
            const docId = document.getElementById('doc-select').value;
            if (!docId) return;
            collabWs.send(JSON.stringify({ type: 'join_document', document_id: docId }));
        }

        function saveDocument(isAutoSave = false) {
            if (!collabWs) return;
            const docId = document.getElementById('doc-select').value;
            const content = document.getElementById('doc-content').value;
            if (!docId) {
                if (!isAutoSave) {
                    addMessage('collab-messages', '❌ Please select a document', 'error');
                }
                return;
            }

            // Update status
            const statusEl = document.getElementById('save-status');
            statusEl.textContent = isAutoSave ? 'Auto-saving...' : 'Saving...';
            statusEl.style.color = '#007bff';

            collabWs.send(JSON.stringify({
                type: 'document_operation',
                document_id: docId,
                operation: isAutoSave ? 'auto_update' : 'manual_update',
                content: content
            }));

            // Clear auto-save timeout
            if (autoSaveTimeout) {
                clearTimeout(autoSaveTimeout);
                autoSaveTimeout = null;
            }

            setTimeout(() => {
                statusEl.textContent = 'Saved';
                statusEl.style.color = '#28a745';
                setTimeout(() => {
                    statusEl.textContent = '';
                }, 2000);
            }, 500);
        }

        function handleCollabMessage(data) {
            switch (data.type) {
                case 'welcome':
                    addMessage('collab-messages', data.message, 'success');
                    updateDocumentList(data.documents);
                    break;
                case 'document_created':
                    addMessage('collab-messages', data.message, 'success');
                    updateDocumentList([data.document]);
                    break;
                case 'document_joined':
                    addMessage('collab-messages', data.message + ' (' + data.active_clients + ' clients active)', 'success');
                    document.getElementById('doc-content').value = data.document.content;
                    updateDocumentVersion(data.document.version);
                    break;
                case 'document_updated':
                    const currentDocSelect = document.getElementById('doc-select').value;
                    if (currentDocSelect === data.document.id) {
                        // Temporarily disable auto-save to prevent conflicts
                        isAutoSaving = true;

                        // Only update if we're viewing this document
                        document.getElementById('doc-content').value = data.document.content;
                        updateDocumentVersion(data.document.version);

                        if (data.editor_id) {
                            addMessage('collab-messages', '✏️ Document updated by ' + data.editor_id + ' (v' + data.document.version + ')', 'info');
                        }

                        // Re-enable auto-save after a short delay
                        setTimeout(() => {
                            isAutoSaving = false;
                        }, 500);
                    }
                    break;
                case 'client_joined':
                    addMessage('collab-messages', '👤 Client ' + data.client_id + ' joined (' + data.active_clients + ' active)', 'info');
                    break;
                case 'client_left':
                    addMessage('collab-messages', '👋 Client ' + data.client_id + ' left (' + data.active_clients + ' active)', 'warning');
                    break;
                case 'cursor_updated':
                    addMessage('collab-messages', 'Cursor positions updated', 'info');
                    break;
                case 'error':
                    addMessage('collab-messages', data.message, 'error');
                    break;
                default:
                    addMessage('collab-messages', JSON.stringify(data), 'info');
            }
        }

        function updateDocumentVersion(version) {
            // Update the document select option to show version
            const select = document.getElementById('doc-select');
            const selectedOption = select.options[select.selectedIndex];
            if (selectedOption && selectedOption.value) {
                const baseText = selectedOption.text.split(' (v')[0];
                selectedOption.text = baseText + ' (v' + version + ')';
            }
        }

        function updateDocumentList(documents) {
            const select = document.getElementById('doc-select');
            documents.forEach(doc => {
                if (![...select.options].some(opt => opt.value === doc.id)) {
                    const option = new Option(doc.id + ' (v' + doc.version + ')', doc.id);
                    select.add(option);
                }
            });
        }

        // Gaming Functions
        function connectGame() {
            if (gameWs) return;
            gameWs = new WebSocket('ws://localhost:8080/ws/game');

            gameWs.onopen = () => {
                updateStatus('game-status', true);
                addMessage('game-messages', '✅ Connected to Gaming Server', 'success');
            };

            gameWs.onmessage = (e) => {
                const data = JSON.parse(e.data);
                handleGameMessage(data);
            };

            gameWs.onclose = () => {
                updateStatus('game-status', false);
                addMessage('game-messages', '❌ Disconnected from Gaming Server', 'error');
                gameWs = null;
            };
        }

        function disconnectGame() {
            if (gameWs) {
                gameWs.close();
            }
        }

        function joinGame() {
            if (!gameWs) return;
            const playerName = document.getElementById('player-name').value;
            const gameId = document.getElementById('game-id').value;
            gameWs.send(JSON.stringify({
                type: 'join_game',
                player_name: playerName,
                game_id: gameId
            }));
        }

        function movePlayer(event) {
            if (!gameWs) return;
            const canvas = document.getElementById('game-canvas');
            const rect = canvas.getBoundingClientRect();
            const x = event.clientX - rect.left;
            const y = event.clientY - rect.top;

            gameWs.send(JSON.stringify({
                type: 'player_move',
                game_id: document.getElementById('game-id').value,
                x: x,
                y: y
            }));

            // Update canvas visually
            const ctx = canvas.getContext('2d');
            ctx.fillStyle = '#007bff';
            ctx.clearRect(0, 0, canvas.width, canvas.height);
            ctx.fillRect(x-5, y-5, 10, 10);
        }

        function sendGameAction(action) {
            if (!gameWs) return;
            gameWs.send(JSON.stringify({
                type: 'game_action',
                game_id: document.getElementById('game-id').value,
                action: action
            }));
        }

        function handleGameMessage(data) {
            switch (data.type) {
                case 'welcome':
                    addMessage('game-messages', data.message, 'success');
                    document.getElementById('room-count').textContent = data.rooms.length;
                    break;
                case 'game_joined':
                    addMessage('game-messages', data.message, 'success');
                    document.getElementById('player-count').textContent = Object.keys(data.room.players).length;
                    break;
                case 'player_moved':
                    addMessage('game-messages', 'Player moved to (' + data.position.x + ', ' + data.position.y + ')', 'info');
                    break;
                case 'game_action_result':
                    addMessage('game-messages', 'Action: ' + data.action + ' - ' + data.result, 'info');
                    break;
                case 'ping':
                    // Send pong back
                    gameWs.send(JSON.stringify({ type: 'pong' }));
                    break;
                case 'error':
                    addMessage('game-messages', data.message, 'error');
                    break;
                default:
                    addMessage('game-messages', JSON.stringify(data), 'info');
            }
        }

        // Reconnection Functions
        function connectReconnect() {
            if (reconnectWs) return;

            // Try to use stored session ID first, then current session
            const storedSessionId = localStorage.getItem('reconnect_session_id');
            const sessionToUse = storedSessionId || currentSessionId;

            let url = 'ws://localhost:8080/ws/reconnect';
            if (sessionToUse) {
                url += '?session_id=' + sessionToUse;
                addMessage('reconnect-messages', '🔄 Attempting to reconnect with session: ' + sessionToUse, 'info');
            } else {
                addMessage('reconnect-messages', '🆕 Creating new session...', 'info');
            }

            reconnectWs = new WebSocket(url);

            reconnectWs.onopen = () => {
                updateStatus('reconnect-status', true);
                addMessage('reconnect-messages', '✅ Connected to Reconnection System', 'success');
            };

            reconnectWs.onmessage = (e) => {
                const data = JSON.parse(e.data);
                handleReconnectMessage(data);
            };

            reconnectWs.onerror = (error) => {
                addMessage('reconnect-messages', '💥 WebSocket error occurred', 'error');
                console.error('Reconnection WebSocket error:', error);
            };

            reconnectWs.onclose = (event) => {
                updateStatus('reconnect-status', false);
                if (event.code === 1000) {
                    // Normal closure (manual disconnect)
                    addMessage('reconnect-messages', '✅ Disconnected successfully (code: ' + event.code + ')', 'success');
                } else {
                    // Abnormal closure
                    addMessage('reconnect-messages', '❌ Connection lost (code: ' + event.code + ', reason: ' + (event.reason || 'Unknown') + ')', 'error');
                }
                reconnectWs = null;
            };
        }

        function loadStoredSession() {
            const storedSessionId = localStorage.getItem('reconnect_session_id');
            if (storedSessionId) {
                currentSessionId = storedSessionId;
                document.getElementById('session-id').textContent = 'Session: ' + storedSessionId + ' (stored)';
                addMessage('reconnect-messages', '💾 Previous session loaded: ' + storedSessionId, 'info');
            }
        }

        function disconnectReconnect() {
            if (reconnectWs && reconnectWs.readyState === WebSocket.OPEN) {
                addMessage('reconnect-messages', '🔌 Manually disconnecting...', 'warning');
                // Force immediate disconnect
                reconnectWs.onclose = null; // Temporarily disable onclose to avoid duplicate messages
                reconnectWs.close(1000, 'Manual disconnect');

                // Manually trigger the disconnect handling
                setTimeout(() => {
                    updateStatus('reconnect-status', false);
                    addMessage('reconnect-messages', '✅ Disconnected successfully (manual)', 'success');
                    reconnectWs = null;
                }, 100);
            } else if (reconnectWs && reconnectWs.readyState === WebSocket.CONNECTING) {
                addMessage('reconnect-messages', '🔌 Disconnecting (was connecting)...', 'warning');
                reconnectWs.close();
            } else {
                addMessage('reconnect-messages', '❌ Already disconnected', 'info');
            }
        }

        function simulateReconnect() {
            disconnectReconnect();
            setTimeout(() => {
                connectReconnect();
            }, 1000);
        }

        function sendQueuedMessage() {
            if (!reconnectWs) return;
            const message = document.getElementById('queue-message').value;
            const targetSession = document.getElementById('target-session').value;

            if (!message || !targetSession) {
                addMessage('reconnect-messages', '❌ Please enter message and target session', 'error');
                return;
            }

            reconnectWs.send(JSON.stringify({
                type: 'send_message',
                target_session: targetSession,
                data: message
            }));

            document.getElementById('queue-message').value = '';
        }

        function getQueuedMessages() {
            if (!reconnectWs) {
                addMessage('reconnect-messages', '❌ Not connected to reconnection system', 'error');
                return;
            }
            if (!currentSessionId) {
                addMessage('reconnect-messages', '❌ No active session', 'error');
                return;
            }

            addMessage('reconnect-messages', '🔍 Checking for queued messages...', 'info');
            reconnectWs.send(JSON.stringify({ type: 'get_queued' }));
        }

        function useMySession() {
            if (currentSessionId) {
                document.getElementById('target-session').value = currentSessionId;
                addMessage('reconnect-messages', '✅ Using your session ID: ' + currentSessionId, 'info');
            } else {
                addMessage('reconnect-messages', '❌ No active session. Connect first!', 'error');
            }
        }

        function clearReconnectMessages() {
            document.getElementById('reconnect-messages').innerHTML = '';
        }

        function quickTest() {
            if (!currentSessionId) {
                addMessage('reconnect-messages', '❌ Connect first to get a session ID!', 'error');
                return;
            }

            const testMessage = 'Test message sent at ' + new Date().toLocaleTimeString();
            document.getElementById('queue-message').value = testMessage;
            document.getElementById('target-session').value = currentSessionId;

            addMessage('reconnect-messages', '🧪 Quick test: queuing message to your own session...', 'info');
            setTimeout(() => {
                sendQueuedMessage();
                setTimeout(() => {
                    addMessage('reconnect-messages', '💡 Now disconnect and reconnect to see the queued message!', 'warning');
                }, 1000);
            }, 500);
        }

        function checkConnectionStatus() {
            const states = ['CONNECTING', 'OPEN', 'CLOSING', 'CLOSED'];
            if (reconnectWs) {
                const state = states[reconnectWs.readyState] || 'UNKNOWN';
                addMessage('reconnect-messages', '🔍 WebSocket state: ' + state + ' (' + reconnectWs.readyState + ')', 'info');
                addMessage('reconnect-messages', '📋 Session: ' + (currentSessionId || 'None'), 'info');
            } else {
                addMessage('reconnect-messages', '🔍 WebSocket: Not initialized', 'info');
                addMessage('reconnect-messages', '📋 Session: ' + (currentSessionId || 'None'), 'info');
            }
            const stored = localStorage.getItem('reconnect_session_id');
            addMessage('reconnect-messages', '💾 Stored: ' + (stored || 'None'), 'info');
        }

        function clearStoredSession() {
            localStorage.removeItem('reconnect_session_id');
            currentSessionId = null;
            document.getElementById('session-id').textContent = '';
            addMessage('reconnect-messages', '🧹 Cleared stored session - next connect will create fresh session', 'warning');
        }

        function handleReconnectMessage(data) {
            switch (data.type) {
                case 'session_created':
                    currentSessionId = data.session_id;
                    localStorage.setItem('reconnect_session_id', data.session_id);
                    document.getElementById('session-id').textContent = 'Session: ' + data.session_id;
                    addMessage('reconnect-messages', data.message + ' 🔗 Session saved for reconnection', 'success');
                    break;
                case 'reconnection_failed':
                    addMessage('reconnect-messages', '⚠️ ' + data.message, 'warning');
                    // Clear the old session ID from storage since it's invalid
                    localStorage.removeItem('reconnect_session_id');
                    break;
                case 'reconnected':
                    currentSessionId = data.session_id;
                    document.getElementById('session-id').textContent = 'Session: ' + data.session_id + ' (reconnected)';
                    addMessage('reconnect-messages', '🎉 ' + data.message, 'success');
                    break;
                case 'message_queued':
                    addMessage('reconnect-messages', '✅ ' + data.message, 'success');
                    break;
                case 'queued_messages':
                    if (data.count > 0) {
                        addMessage('reconnect-messages', '📬 Retrieved ' + data.count + ' queued messages:', 'info');
                        data.messages.forEach((msg, index) => {
                            addMessage('reconnect-messages', '📨 #' + (index + 1) + ': ' + JSON.stringify(msg.data) + ' (queued at ' + new Date(msg.timestamp).toLocaleTimeString() + ')', 'warning');
                        });
                    } else {
                        addMessage('reconnect-messages', '📭 No queued messages found', 'info');
                    }
                    break;
                case 'queued_message':
                    addMessage('reconnect-messages', '📨 Received queued message: ' + JSON.stringify(data.data) + ' (sent at ' + new Date(data.queued_at).toLocaleTimeString() + ')', 'warning');
                    break;
                case 'error':
                    addMessage('reconnect-messages', '❌ ' + data.message, 'error');
                    break;
                default:
                    addMessage('reconnect-messages', JSON.stringify(data), 'info');
            }
        }

        // Multi-Protocol Functions
        function connectMultiProto() {
            const protocol = document.getElementById('protocol-select').value;

            // Disconnect existing connection if switching protocols
            if (multiprotoWs) {
                addMessage('multiproto-messages', '🔄 Switching to ' + protocol + ' protocol...', 'info');
                multiprotoWs.close();
                multiprotoWs = null;
                // Give a brief moment for the old connection to close
                setTimeout(() => createMultiProtoConnection(protocol), 200);
            } else {
                createMultiProtoConnection(protocol);
            }
        }

        function createMultiProtoConnection(protocol) {
            addMessage('multiproto-messages', '🔗 Connecting with ' + protocol + ' protocol...', 'info');
            multiprotoWs = new WebSocket('ws://localhost:8080/ws/multiproto?protocol=' + protocol);

            multiprotoWs.onopen = () => {
                updateStatus('multiproto-status', true);
                addMessage('multiproto-messages', '✅ Connected with ' + protocol + ' protocol', 'success');
                // Request updated stats after connection
                setTimeout(() => {
                    if (multiprotoWs) getProtocolStats();
                }, 500);
            };

            multiprotoWs.onmessage = (e) => {
                const data = JSON.parse(e.data);
                handleMultiProtoMessage(data);
            };

            multiprotoWs.onerror = (error) => {
                addMessage('multiproto-messages', '💥 Connection error with ' + protocol + ' protocol', 'error');
            };

            multiprotoWs.onclose = () => {
                updateStatus('multiproto-status', false);
                addMessage('multiproto-messages', '❌ Disconnected from Multi-Protocol System', 'error');
                multiprotoWs = null;
            };
        }

        function disconnectMultiProto() {
            if (multiprotoWs) {
                multiprotoWs.close();
            }
        }

        function protocolChanged() {
            const protocol = document.getElementById('protocol-select').value;
            addMessage('multiproto-messages', '📝 Protocol changed to: ' + protocol + '. Click Connect to switch.', 'info');

            // Auto-connect if already connected
            if (multiprotoWs && multiprotoWs.readyState === WebSocket.OPEN) {
                connectMultiProto();
            }
        }

        function sendProtocolPing() {
            if (!multiprotoWs) return;
            multiprotoWs.send(JSON.stringify({ type: 'ping' }));
        }

        function sendEcho() {
            if (!multiprotoWs) return;
            const message = document.getElementById('echo-message').value;
            multiprotoWs.send(JSON.stringify({ type: 'echo', data: message }));
            document.getElementById('echo-message').value = '';
        }

        function getProtocolStats() {
            if (!multiprotoWs) return;
            multiprotoWs.send(JSON.stringify({ type: 'stats' }));
        }

        function resetProtocolStats() {
            if (!multiprotoWs) {
                addMessage('multiproto-messages', '❌ Not connected', 'error');
                return;
            }
            addMessage('multiproto-messages', '🧹 Resetting protocol statistics...', 'info');
            multiprotoWs.send(JSON.stringify({ type: 'reset_stats' }));
        }

        function handleMultiProtoMessage(data) {
            switch (data.type) {
                case 'welcome':
                    addMessage('multiproto-messages', data.message, 'success');
                    updateProtocolStats(data.stats);
                    break;
                case 'pong':
                    addMessage('multiproto-messages', 'Pong received from ' + data.protocol + ' protocol', 'info');
                    break;
                case 'echo_response':
                    addMessage('multiproto-messages', 'Echo: ' + JSON.stringify(data.original_data), 'info');
                    break;
                case 'protocol_stats':
                    updateProtocolStats(data.stats);
                    addMessage('multiproto-messages', 'Protocol stats updated', 'info');
                    break;
                case 'stats_reset':
                    addMessage('multiproto-messages', '✅ ' + data.message, 'success');
                    updateProtocolStats(data.stats);
                    break;
                case 'error':
                    addMessage('multiproto-messages', data.message, 'error');
                    break;
                default:
                    addMessage('multiproto-messages', JSON.stringify(data), 'info');
            }
        }

        function updateProtocolStats(stats) {
            document.getElementById('json-count').textContent = stats.json || 0;
            document.getElementById('binary-count').textContent = stats.binary || 0;
            document.getElementById('msgpack-count').textContent = stats.msgpack || 0;
        }

        // Initialize
        window.onload = function() {
            addMessage('files-messages', '🚀 File Transfer System ready', 'info');
            addMessage('collab-messages', '🚀 Collaboration System ready', 'info');
            addMessage('game-messages', '🚀 Gaming Server ready', 'info');
            addMessage('reconnect-messages', '🚀 Reconnection System ready', 'info');
            addMessage('multiproto-messages', '🚀 Multi-Protocol System ready', 'info');

            // Load stored session for reconnection testing
            loadStoredSession();
        };
    </script>
</body>
</html>`

	ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
	return ctx.Send([]byte(html))
}

func statsHandler(ctx router.Context) error {
	fileStorageMutex.RLock()
	fileCount := len(fileStorage)
	fileStorageMutex.RUnlock()

	documentMutex.RLock()
	docCount := len(documentState)
	documentMutex.RUnlock()

	gameRoomsMutex.RLock()
	gameCount := len(gameRooms)
	gameRoomsMutex.RUnlock()

	sessionsMutex.RLock()
	sessionCount := len(sessions)
	sessionsMutex.RUnlock()

	protocolMutex.RLock()
	protocolStats := make(map[string]int)
	for k, v := range protocolStats {
		protocolStats[k] = v
	}
	protocolMutex.RUnlock()

	return ctx.JSON(200, map[string]any{
		"files": map[string]any{
			"count":      fileCount,
			"total_size": getTotalFileSize(),
		},
		"documents": map[string]any{
			"count": docCount,
		},
		"games": map[string]any{
			"rooms":         gameCount,
			"total_players": getTotalPlayers(),
		},
		"sessions": map[string]any{
			"count":  sessionCount,
			"active": getActiveSessions(),
		},
		"protocols": protocolStats,
		"timestamp": time.Now().Unix(),
	})
}

func getTotalFileSize() int64 {
	fileStorageMutex.RLock()
	defer fileStorageMutex.RUnlock()

	var total int64
	for _, file := range fileStorage {
		total += file.Size
	}
	return total
}

func getTotalPlayers() int {
	gameRoomsMutex.RLock()
	defer gameRoomsMutex.RUnlock()

	var total int
	for _, room := range gameRooms {
		total += len(room.Players)
	}
	return total
}

func getActiveSessions() int {
	sessionsMutex.RLock()
	defer sessionsMutex.RUnlock()

	var active int
	for _, session := range sessions {
		if session.Connected {
			active++
		}
	}
	return active
}
