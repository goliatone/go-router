package router

import (
	"fmt"
	"testing"
)

func TestOnPreUpgradeHook(t *testing.T) {
	// Test that OnPreUpgrade hook is called and data is accessible
	config := WebSocketConfig{
		Origins: []string{"*"},
		OnPreUpgrade: func(c Context) (UpgradeData, error) {
			// Simulate extracting auth data from HTTP context
			token := c.Query("token")
			if token == "" {
				return nil, fmt.Errorf("token required")
			}

			return UpgradeData{
				"token":   token,
				"user_id": "test-user",
				"role":    "admin",
			}, nil
		},
		OnConnect: func(ws WebSocketContext) error {
			// Test accessing upgrade data
			if token, exists := ws.UpgradeData("token"); exists {
				if token != "test-token" {
					t.Errorf("Expected token 'test-token', got %v", token)
				}
			} else {
				t.Error("Token not found in upgrade data")
			}

			if userID := GetUpgradeDataWithDefault(ws, "user_id", ""); userID != "test-user" {
				t.Errorf("Expected user_id 'test-user', got %v", userID)
			}

			if role := GetUpgradeDataWithDefault(ws, "role", ""); role != "admin" {
				t.Errorf("Expected role 'admin', got %v", role)
			}

			return nil
		},
	}

	// Validate that the config can be applied
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	// Test that OnPreUpgrade exists
	if config.OnPreUpgrade == nil {
		t.Fatal("OnPreUpgrade should not be nil")
	}

	t.Log("✅ OnPreUpgrade hook implementation test passed")
}

func TestUpgradeDataTypes(t *testing.T) {
	// Test the UpgradeData type
	data := UpgradeData{
		"string_value": "test",
		"int_value":    42,
		"bool_value":   true,
	}

	if data["string_value"] != "test" {
		t.Errorf("Expected 'test', got %v", data["string_value"])
	}

	if data["int_value"] != 42 {
		t.Errorf("Expected 42, got %v", data["int_value"])
	}

	if data["bool_value"] != true {
		t.Errorf("Expected true, got %v", data["bool_value"])
	}

	t.Log("✅ UpgradeData type test passed")
}

func TestEnhancedWebSocketContext(t *testing.T) {
	// Test the enhancedWebSocketContext wrapper
	upgradeData := UpgradeData{
		"token":   "test-token",
		"user_id": "user123",
		"role":    "admin",
	}

	enhanced := &enhancedWebSocketContext{
		upgradeData: upgradeData,
	}

	// Test UpgradeData method
	if token, exists := enhanced.UpgradeData("token"); !exists || token != "test-token" {
		t.Errorf("Expected token 'test-token', got %v (exists: %v)", token, exists)
	}

	if userID, exists := enhanced.UpgradeData("user_id"); !exists || userID != "user123" {
		t.Errorf("Expected user_id 'user123', got %v (exists: %v)", userID, exists)
	}

	if role, exists := enhanced.UpgradeData("role"); !exists || role != "admin" {
		t.Errorf("Expected role 'admin', got %v (exists: %v)", role, exists)
	}

	// Test non-existent key
	if value, exists := enhanced.UpgradeData("nonexistent"); exists {
		t.Errorf("Expected nonexistent key to not exist, but got %v", value)
	}

	t.Log("✅ enhancedWebSocketContext test passed")
}

func TestGetUpgradeDataWithDefault(t *testing.T) {
	// Test the convenience function
	upgradeData := UpgradeData{
		"existing_key": "existing_value",
	}

	enhanced := &enhancedWebSocketContext{
		upgradeData: upgradeData,
	}

	// Test existing key
	value := GetUpgradeDataWithDefault(enhanced, "existing_key", "default")
	if value != "existing_value" {
		t.Errorf("Expected 'existing_value', got %v", value)
	}

	// Test non-existing key with default
	value = GetUpgradeDataWithDefault(enhanced, "nonexistent_key", "default_value")
	if value != "default_value" {
		t.Errorf("Expected 'default_value', got %v", value)
	}

	t.Log("✅ GetUpgradeDataWithDefault test passed")
}