# Websocket Advanced

Scenario: Industrial IoT Data Collection System

Context: You're building a WebSocket server that collects real-time sensor data from industrial
equipment (temperature sensors, pressure gauges, motor controllers, etc.). Each device type uses
a different data protocol and has specific requirements:

- Temperature sensors: Send compressed binary data every 100ms
- Pressure gauges: Use JSON with custom validation and specific compression thresholds
- Motor controllers: Implement a custom binary protocol with acknowledgments
- Legacy devices: Use a proprietary text-based protocol

Why Advanced is Better Here:
1. Multiple custom protocols - Each device type needs different handling
2. Custom compression strategies - Binary data benefits from different compression than JSON
3. Strict timeout management - Industrial systems need precise timing
4. Custom validation - Each protocol has specific validation requirements
5. Performance critical - High-frequency data requires minimal overhead

Implementation Example
```go
package main

import (
    "context"
    "encoding/binary"
    "encoding/json"
    "fmt"
    "log"
    "strings"
    "time"

    "github.com/goliatone/go-router"
)

// Industrial IoT WebSocket Server
func main() {
    app := router.NewHTTPServer()

    // Create protocol negotiator for different device types
    negotiator := router.NewSubprotocolNegotiator()

    // Register temperature sensor protocol (binary, high frequency)
    negotiator.Register(router.SubprotocolHandler{
        Name:    "temp-sensor-v1",
        Version: "1.0",
        Validate: func(ctx router.WebSocketContext) error {
            // Validate device ID is present in query params
            deviceID := ctx.Query("device_id")
            if deviceID == "" || !strings.HasPrefix(deviceID, "TEMP_") {
                return fmt.Errorf("invalid temperature sensor device ID")
            }
            return nil
        },
        Initialize: func(ctx router.WebSocketContext) error {
            // Set up temperature sensor specific settings
            ctx.SetReadDeadline(time.Now().Add(200 * time.Millisecond)) // Fast timeout
            log.Printf("Temperature sensor %s connected", ctx.Query("device_id"))
            return nil
        },
        OnMessage: handleTemperatureSensorData,
    })

    // Register pressure gauge protocol (JSON with custom compression)
    negotiator.Register(router.SubprotocolHandler{
        Name:    "pressure-gauge-v2",
        Version: "2.0",
        Validate: func(ctx router.WebSocketContext) error {
            // Validate authentication token for pressure gauges
            token := ctx.Query("auth_token")
            if !isValidPressureGaugeToken(token) {
                return fmt.Errorf("invalid pressure gauge authentication")
            }
            return nil
        },
        Initialize: func(ctx router.WebSocketContext) error {
            ctx.SetReadDeadline(time.Now().Add(5 * time.Second)) // Slower timeout
            log.Printf("Pressure gauge %s connected", ctx.Query("device_id"))
            return nil
        },
        OnMessage: handlePressureGaugeData,
    })

    // Register motor controller protocol (custom binary with ACKs)
    negotiator.Register(router.SubprotocolHandler{
        Name:    "motor-controller-v1",
        Version: "1.0",
        Validate: validateMotorController,
        Initialize: initializeMotorController,
        OnMessage: handleMotorControllerData,
    })

    // Create custom upgrader with advanced features
    upgraderConfig := router.CustomUpgraderConfig{
        ReadBufferSize:   8192,  // Larger buffer for industrial data
        WriteBufferSize:  8192,
        HandshakeTimeout: 30 * time.Second, // Longer timeout for slow industrial networks
        Subprotocols:     negotiator.GetSupportedProtocols(),
        EnableCompression: true,

        // Custom validation before upgrade
        BeforeUpgrade: func(ctx router.Context) error {
            // Validate industrial device certificates or API keys
            apiKey := ctx.Header("X-Industrial-API-Key")
            if !isValidIndustrialDevice(apiKey) {
                return fmt.Errorf("unauthorized industrial device")
            }

            // Log device connection attempt
            deviceType := ctx.Query("device_type")
            deviceID := ctx.Query("device_id")
            log.Printf("Industrial device connecting: Type=%s, ID=%s", deviceType, deviceID)

            return nil
        },

        // Configure device-specific settings after upgrade
        AfterUpgrade: func(wsCtx router.WebSocketContext) error {
            protocol := wsCtx.Subprotocol()

            // Configure compression based on device type
            if strings.Contains(protocol, "temp-sensor") {
                // Temperature sensors benefit from aggressive compression
                configureHighCompression(wsCtx)
            } else if strings.Contains(protocol, "motor-controller") {
                // Motor controllers need minimal compression for speed
                configureMinimalCompression(wsCtx)
            }

            return nil
        },

        ValidateOrigin: func(origin string) bool {
            // Only allow connections from industrial network
            return strings.HasSuffix(origin, ".factory.local") ||
                    strings.HasSuffix(origin, ".industrial.company.com")
        },

        ErrorHandler: func(ctx router.Context, err error) error {
            // Log industrial device connection errors for monitoring
            log.Printf("Industrial device connection failed: %v", err)

            // Return appropriate error response
            return ctx.Status(400).JSON(map[string]string{
                "error": "Industrial device connection failed",
                "code":  "DEVICE_CONNECTION_ERROR",
            })
        },
    }

    // Create advanced compression manager with device-specific thresholds
    compressionConfig := router.CompressionConfig{
        Enabled:   true,
        Level:     6, // Balanced compression
        Threshold: 512, // Compress messages > 512 bytes
    }
    compressionManager := router.NewCompressionManager(compressionConfig)

    // Create custom deadline manager for industrial timing requirements
    wsConfig := router.WebSocketConfig{
        PingPeriod: 30 * time.Second,  // Longer ping for industrial networks
        PongWait:   60 * time.Second,  // Accommodate network delays
    }

    // Industrial WebSocket handler
    industrialHandler := func(ctx router.Context) error {
        upgrader := router.NewCustomUpgrader(upgraderConfig)

        // Perform custom upgrade
        wsCtx, err := upgrader.Upgrade(ctx)
        if err != nil {
            return err
        }
        defer wsCtx.Close()

        // Set up deadline management for industrial requirements
        deadlineManager := router.NewDeadlineManager(wsCtx, wsConfig)
        deadlineManager.Start()
        defer deadlineManager.Stop()

        // Handle the connection based on negotiated protocol
        if wsCtx.Subprotocol() != "" {
            return negotiator.HandleConnection(wsCtx)
        }

        // Fallback for devices without protocol negotiation
        return handleLegacyDevice(wsCtx)
    }

    // Register the industrial IoT endpoint
    app.Router().Get("/ws/industrial", industrialHandler)

    // Health check endpoint for monitoring
    app.Router().Get("/health", func(ctx router.Context) error {
        return ctx.JSON(200, map[string]any{
            "status": "healthy",
            "active_devices": getActiveDeviceCount(),
            "protocols": negotiator.GetSupportedProtocols(),
        })
    })

    log.Printf("Industrial IoT WebSocket server starting on :8080")
    log.Printf("Supported protocols: %v", negotiator.GetSupportedProtocols())

    if err := app.Serve(":8080"); err != nil {
        log.Fatal(err)
    }
}

// Temperature sensor handler - binary protocol
func handleTemperatureSensorData(ctx router.WebSocketContext, msgType int, data []byte) error {
    if msgType != router.BinaryMessage {
        return fmt.Errorf("temperature sensors must send binary data")
    }

    // Parse binary temperature data (custom protocol)
    if len(data) < 12 { // deviceID(4) + timestamp(4) + temperature(4)
        return fmt.Errorf("invalid temperature data length")
    }

    deviceID := binary.BigEndian.Uint32(data[0:4])
    timestamp := binary.BigEndian.Uint32(data[4:8])
    temperature := binary.BigEndian.Uint32(data[8:12])

    // Convert to actual temperature (custom scaling)
    actualTemp := float32(temperature) / 100.0 // 2 decimal places

    // Store in time-series database (high-frequency data)
    err := storeTemperatureReading(deviceID, timestamp, actualTemp)
    if err != nil {
        log.Printf("Failed to store temperature reading: %v", err)
    }

    // Send acknowledgment (binary response)
    ackData := make([]byte, 8)
    binary.BigEndian.PutUint32(ackData[0:4], deviceID)
    binary.BigEndian.PutUint32(ackData[4:8], timestamp)

    return ctx.WriteMessage(router.BinaryMessage, ackData)
}

// Pressure gauge handler - JSON protocol with validation
func handlePressureGaugeData(ctx router.WebSocketContext, msgType int, data []byte) error {
    if msgType != router.TextMessage {
        return fmt.Errorf("pressure gauges must send JSON data")
    }

    // Parse JSON with custom validation
    var reading PressureReading
    if err := json.Unmarshal(data, &reading); err != nil {
        return fmt.Errorf("invalid pressure gauge JSON: %w", err)
    }

    // Validate pressure reading
    if err := reading.Validate(); err != nil {
        ctx.WriteJSON(map[string]string{
            "error": err.Error(),
            "type":  "validation_error",
        })
        return nil // Don't disconnect for validation errors
    }

    // Store reading
    if err := storePressureReading(reading); err != nil {
        log.Printf("Failed to store pressure reading: %v", err)
    }

    // Send confirmation
    return ctx.WriteJSON(map[string]any{
        "status":    "ok",
        "timestamp": time.Now().Unix(),
        "device_id": reading.DeviceID,
    })
}

// Motor controller handler - complex binary protocol with commands
func handleMotorControllerData(ctx router.WebSocketContext, msgType int, data []byte) error {
    if msgType != router.BinaryMessage {
        return fmt.Errorf("motor controllers must send binary data")
    }

    // Parse motor control protocol (more complex)
    cmd, err := parseMotorCommand(data)
    if err != nil {
        return fmt.Errorf("invalid motor command: %w", err)
    }

    // Execute motor command
    result, err := executeMotorCommand(cmd)
    if err != nil {
        // Send error response
        errorResponse := encodeMotorError(cmd.CommandID, err)
        return ctx.WriteMessage(router.BinaryMessage, errorResponse)
    }

    // Send success response
    successResponse := encodeMotorResponse(cmd.CommandID, result)
    return ctx.WriteMessage(router.BinaryMessage, successResponse)
}

// Supporting types and functions
type PressureReading struct {
    DeviceID  string    `json:"device_id"`
    Pressure  float64   `json:"pressure"`
    Unit      string    `json:"unit"`
    Timestamp time.Time `json:"timestamp"`
    Location  string    `json:"location"`
}

func (p PressureReading) Validate() error {
    if p.DeviceID == "" {
        return fmt.Errorf("device_id required")
    }
    if p.Pressure < 0 || p.Pressure > 10000 { // PSI limits
        return fmt.Errorf("pressure out of range: %f", p.Pressure)
    }
    if p.Unit != "PSI" && p.Unit != "bar" && p.Unit != "kPa" {
        return fmt.Errorf("invalid pressure unit: %s", p.Unit)
    }
    return nil
}

type MotorCommand struct {
    CommandID uint32
    DeviceID  uint32
    Command   uint8  // START, STOP, SPEED_CHANGE, etc.
    Value     uint32 // Speed, position, etc.
}

type MotorResult struct {
    Status   uint8
    Value    uint32
    ErrorCode uint16
}

// Helper functions (simplified implementations)
func isValidIndustrialDevice(apiKey string) bool {
    return apiKey != "" && strings.HasPrefix(apiKey, "IND_")
}

func isValidPressureGaugeToken(token string) bool {
    return token != "" && len(token) >= 10
}

func validateMotorController(ctx router.WebSocketContext) error {
    controllerID := ctx.Query("controller_id")
    if !strings.HasPrefix(controllerID, "MOTOR_") {
        return fmt.Errorf("invalid motor controller ID")
    }
    return nil
}

func initializeMotorController(ctx router.WebSocketContext) error {
    // Set very short timeouts for motor controllers (real-time requirements)
    ctx.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
    log.Printf("Motor controller %s connected", ctx.Query("controller_id"))
    return nil
}

func configureHighCompression(ctx router.WebSocketContext) {
    // Configure aggressive compression for temperature data
    log.Printf("Configured high compression for device %s", ctx.ConnectionID())
}

func configureMinimalCompression(ctx router.WebSocketContext) {
    // Configure minimal compression for motor controllers
    log.Printf("Configured minimal compression for device %s", ctx.ConnectionID())
}

func handleLegacyDevice(ctx router.WebSocketContext) error {
    // Handle devices that don't support protocol negotiation
    log.Printf("Handling legacy device %s", ctx.ConnectionID())

    for {
        msgType, data, err := ctx.ReadMessage()
        if err != nil {
            return err
        }

        // Simple echo for legacy devices
        err = ctx.WriteMessage(msgType, data)
        if err != nil {
            return err
        }
    }
}

func getActiveDeviceCount() int {
    return 42 // Mock implementation
}

func storeTemperatureReading(deviceID uint32, timestamp uint32, temp float32) error {
    log.Printf("Storing temp reading: Device=%d, Temp=%.2f°C", deviceID, temp)
    return nil
}

func storePressureReading(reading PressureReading) error {
    log.Printf("Storing pressure reading: %+v", reading)
    return nil
}

func parseMotorCommand(data []byte) (*MotorCommand, error) {
    if len(data) < 13 {
        return nil, fmt.Errorf("insufficient data length")
    }

    return &MotorCommand{
        CommandID: binary.BigEndian.Uint32(data[0:4]),
        DeviceID:  binary.BigEndian.Uint32(data[4:8]),
        Command:   data[8],
        Value:     binary.BigEndian.Uint32(data[9:13]),
    }, nil
}

func executeMotorCommand(cmd *MotorCommand) (*MotorResult, error) {
    // Mock motor command execution
    return &MotorResult{
        Status:    1, // SUCCESS
        Value:     cmd.Value,
        ErrorCode: 0,
    }, nil
}

func encodeMotorResponse(commandID uint32, result *MotorResult) []byte {
    data := make([]byte, 11)
    binary.BigEndian.PutUint32(data[0:4], commandID)
    data[4] = result.Status
    binary.BigEndian.PutUint32(data[5:9], result.Value)
    binary.BigEndian.PutUint16(data[9:11], result.ErrorCode)
    return data
}

func encodeMotorError(commandID uint32, err error) []byte {
    data := make([]byte, 11)
    binary.BigEndian.PutUint32(data[0:4], commandID)
    data[4] = 0 // ERROR status
    // Error code based on error type
    binary.BigEndian.PutUint16(data[9:11], 500) // Generic error
    return data
}
```

Why Advanced is Superior Here

1. Multiple Protocol Support
```go
// Easy approach would require separate endpoints for each device type
app.Router().Get("/ws/temp", tempHandler)    // Different handlers
app.Router().Get("/ws/pressure", pressureHandler)
app.Router().Get("/ws/motor", motorHandler)

// Advanced approach uses protocol negotiation at single endpoint
app.Router().Get("/ws/industrial", industrialHandler) // One endpoint, multiple protocols
```

2. Custom Validation & Security
```go
// Advanced allows custom validation per protocol
BeforeUpgrade: func(ctx router.Context) error {
    return validateIndustrialCertificate(ctx)
},
ValidateOrigin: func(origin string) bool {
    return isFromIndustrialNetwork(origin)
}
```

3. Performance Optimization
```go
// Different compression strategies per device type
if strings.Contains(protocol, "temp-sensor") {
    configureHighCompression(wsCtx)  // Binary data compresses well
} else if strings.Contains(protocol, "motor-controller") {
    configureMinimalCompression(wsCtx)  // Real-time needs speed
}
```
4. Precise Timeout Management

```go
// Different timeout requirements per device
if deviceType == "motor-controller" {
    ctx.SetReadDeadline(time.Now().Add(50 * time.Millisecond))  // Real-time
} else if deviceType == "temp-sensor" {
    ctx.SetReadDeadline(time.Now().Add(200 * time.Millisecond)) // High frequency
}
```

Benefits Over Easy Approach

1. Single Endpoint - One URL handles all device types through protocol negotiation
2. Device-Specific Optimization - Each protocol optimized for its use case
3. Custom Validation - Protocol-specific security and validation rules
4. Performance Tuning - Different compression/timeout strategies per device
5. Industrial Requirements - Precise timing, binary protocols, acknowledgments
6. Monitoring & Diagnostics - Custom error handling and logging per protocol

This scenario demonstrates how the Advanced approach provides the granular control needed for
complex, performance-critical systems that the Easy approach simply cannot handle effectively.
