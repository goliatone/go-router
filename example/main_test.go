package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupFiberTest() (router.Server[*fiber.App], *UserStore) {
	app := newFiberAdapter()
	store := NewUserStore()
	createRoutes(app, store)
	return app, store
}

func TestFiber_ListUsers(t *testing.T) {
	app, _ := setupFiberTest()

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	resp, err := app.WrappedRouter().Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var users []User
	err = json.NewDecoder(resp.Body).Decode(&users)
	require.NoError(t, err)
	assert.Len(t, users, 3)

	for _, user := range users {
		assert.NotEmpty(t, user.ID)
		assert.NotEmpty(t, user.Name)
		assert.NotEmpty(t, user.Email)
		assert.False(t, user.CreatedAt.IsZero())
		assert.False(t, user.UpdatedAt.IsZero())
	}
}

func TestFiber_CreateUser(t *testing.T) {
	app, _ := setupFiberTest()

	tests := []struct {
		name       string
		payload    CreateUserRequest
		wantStatus int
		wantErr    bool
	}{
		{
			name: "valid user",
			payload: CreateUserRequest{
				Name:  "Test User",
				Email: "test.user@example.com",
			},
			wantStatus: http.StatusCreated,
			wantErr:    false,
		},
		{
			name: "missing name",
			payload: CreateUserRequest{
				Email: "test.user@example.com",
			},
			wantStatus: http.StatusBadRequest,
			wantErr:    true,
		},
		{
			name: "missing email",
			payload: CreateUserRequest{
				Name: "Test User",
			},
			wantStatus: http.StatusBadRequest,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payloadBytes, err := json.Marshal(tt.payload)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(payloadBytes))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.WrappedRouter().Test(req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp.StatusCode)

			if !tt.wantErr {
				var user User
				err = json.NewDecoder(resp.Body).Decode(&user)
				require.NoError(t, err)
				assert.Equal(t, tt.payload.Name, user.Name)
				assert.Equal(t, tt.payload.Email, user.Email)
				assert.NotEmpty(t, user.ID)
				assert.False(t, user.CreatedAt.IsZero())
				assert.False(t, user.UpdatedAt.IsZero())
			}
		})
	}
}

func TestFiber_GetUser(t *testing.T) {
	app, store := setupFiberTest()

	var testUser User
	for _, u := range store.users {
		testUser = u
		break
	}

	tests := []struct {
		name       string
		userID     string
		wantStatus int
		wantErr    bool
	}{
		{
			name:       "existing user",
			userID:     testUser.ID,
			wantStatus: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "non existent user",
			userID:     "non-existent-id",
			wantStatus: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/users/"+tt.userID, nil)
			resp, err := app.WrappedRouter().Test(req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp.StatusCode)

			if !tt.wantErr {
				var user User
				err = json.NewDecoder(resp.Body).Decode(&user)
				require.NoError(t, err)
				assert.Equal(t, testUser.ID, user.ID)
				assert.Equal(t, testUser.Name, user.Name)
				assert.Equal(t, testUser.Email, user.Email)
			}
		})
	}
}

func TestFiber_UpdateUser(t *testing.T) {
	app, store := setupFiberTest()

	var testUser User
	for _, u := range store.users {
		testUser = u
		break
	}

	tests := []struct {
		name       string
		userID     string
		payload    UpdateUserRequest
		wantStatus int
		wantErr    bool
	}{
		{
			name:   "valid update",
			userID: testUser.ID,
			payload: UpdateUserRequest{
				Name:  "Updated Name",
				Email: "updated.email@example.com",
			},
			wantStatus: http.StatusOK,
			wantErr:    false,
		},
		{
			name:   "partial update - name only",
			userID: testUser.ID,
			payload: UpdateUserRequest{
				Name: "New Name",
			},
			wantStatus: http.StatusOK,
			wantErr:    false,
		},
		{
			name:   "non existent user",
			userID: "non-existent-id",
			payload: UpdateUserRequest{
				Name: "New Name",
			},
			wantStatus: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payloadBytes, err := json.Marshal(tt.payload)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPut, "/api/users/"+tt.userID, bytes.NewReader(payloadBytes))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.WrappedRouter().Test(req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp.StatusCode)

			if !tt.wantErr {
				var user User
				err = json.NewDecoder(resp.Body).Decode(&user)
				require.NoError(t, err)

				if tt.payload.Name != "" {
					assert.Equal(t, tt.payload.Name, user.Name)
				}
				if tt.payload.Email != "" {
					assert.Equal(t, tt.payload.Email, user.Email)
				}
				assert.True(t, user.UpdatedAt.After(user.CreatedAt))
			}
		})
	}
}

func TestFiber_DeleteUser(t *testing.T) {
	app, store := setupFiberTest()

	var testUser User
	for _, u := range store.users {
		testUser = u
		break
	}

	tests := []struct {
		name       string
		userID     string
		wantStatus int
	}{
		{
			name:       "existing user",
			userID:     testUser.ID,
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "non existent user",
			userID:     "non-existent-id",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/api/users/"+tt.userID, nil)
			resp, err := app.WrappedRouter().Test(req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp.StatusCode)

			if tt.wantStatus == http.StatusNoContent {
				store.RLock()
				_, exists := store.users[tt.userID]
				store.RUnlock()
				assert.False(t, exists)
			}
		})
	}
}

func TestFiber_MethodNotAllowed(t *testing.T) {
	app, _ := setupFiberTest()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{
			name:       "PATCH not allowed on users",
			method:     http.MethodPatch,
			path:       "/api/users",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "HEAD not allowed on users",
			method:     http.MethodHead,
			path:       "/api/users",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			resp, err := app.WrappedRouter().Test(req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp.StatusCode)

		})
	}
}

func setupHTTPServerTest() (*httptest.Server, *UserStore) {
	app := newHTTPServerAdapter()
	store := NewUserStore()
	createRoutes(app, store)
	ts := httptest.NewServer(app.WrappedRouter())
	return ts, store
}

func TestHTTP_ListUsers(t *testing.T) {
	ts, _ := setupHTTPServerTest()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/users")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var users []User
	err = json.NewDecoder(resp.Body).Decode(&users)
	require.NoError(t, err)
	assert.Len(t, users, 3)

	for _, user := range users {
		assert.NotEmpty(t, user.ID)
		assert.NotEmpty(t, user.Name)
		assert.NotEmpty(t, user.Email)
		assert.False(t, user.CreatedAt.IsZero())
		assert.False(t, user.UpdatedAt.IsZero())
	}
}

func TestHTTP_CreateUser(t *testing.T) {
	ts, _ := setupHTTPServerTest()
	defer ts.Close()

	tests := []struct {
		name       string
		payload    CreateUserRequest
		wantStatus int
		wantErr    bool
	}{
		{
			name: "valid user",
			payload: CreateUserRequest{
				Name:  "Test User",
				Email: "test.user@example.com",
			},
			wantStatus: http.StatusCreated,
			wantErr:    false,
		},
		{
			name: "missing name",
			payload: CreateUserRequest{
				Email: "test.user@example.com",
			},
			wantStatus: http.StatusBadRequest,
			wantErr:    true,
		},
		{
			name: "missing email",
			payload: CreateUserRequest{
				Name: "Test User",
			},
			wantStatus: http.StatusBadRequest,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payloadBytes, err := json.Marshal(tt.payload)
			require.NoError(t, err)

			resp, err := http.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(payloadBytes))
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp.StatusCode)

			if !tt.wantErr {
				var user User
				err = json.NewDecoder(resp.Body).Decode(&user)
				require.NoError(t, err)
				assert.Equal(t, tt.payload.Name, user.Name)
				assert.Equal(t, tt.payload.Email, user.Email)
				assert.NotEmpty(t, user.ID)
				assert.False(t, user.CreatedAt.IsZero())
				assert.False(t, user.UpdatedAt.IsZero())
			}
		})
	}
}

func TestHTTP_GetUser(t *testing.T) {
	ts, store := setupHTTPServerTest()
	defer ts.Close()

	// Since we don't know the ID just range and get first :/
	var testUser User
	for _, u := range store.users {
		testUser = u
		break
	}

	tests := []struct {
		name       string
		userID     string
		wantStatus int
		wantErr    bool
	}{
		{
			name:       "existing user " + testUser.Email,
			userID:     testUser.ID,
			wantStatus: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "non existent user",
			userID:     "non-existent-id",
			wantStatus: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/api/users/" + tt.userID)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp.StatusCode)

			if !tt.wantErr {
				var user User
				err := json.NewDecoder(resp.Body).Decode(&user)
				require.NoError(t, err)
				assert.Equal(t, testUser.ID, user.ID, "matches user ID")
				assert.Equal(t, testUser.Name, user.Name, "matches user Name")
				assert.Equal(t, testUser.Email, user.Email, "matches Email")
			}
		})
	}
}

func TestHTTP_UpdateUser(t *testing.T) {
	ts, store := setupHTTPServerTest()
	defer ts.Close()

	var testUser User
	for _, u := range store.users {
		testUser = u
		break
	}

	tests := []struct {
		name       string
		userID     string
		payload    UpdateUserRequest
		wantStatus int
		wantErr    bool
	}{
		{
			name:   "valid update",
			userID: testUser.ID,
			payload: UpdateUserRequest{
				Name:  "Updated Name",
				Email: "updated.email@example.com",
			},
			wantStatus: http.StatusOK,
			wantErr:    false,
		},
		{
			name:   "partial update - name only",
			userID: testUser.ID,
			payload: UpdateUserRequest{
				Name: "New Name",
			},
			wantStatus: http.StatusOK,
			wantErr:    false,
		},
		{
			name:   "non existent user",
			userID: "non-existent-id",
			payload: UpdateUserRequest{
				Name: "New Name",
			},
			wantStatus: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payloadBytes, err := json.Marshal(tt.payload)
			require.NoError(t, err)

			req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/users/"+tt.userID, bytes.NewReader(payloadBytes))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{}
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)

			if !tt.wantErr {
				var user User
				err = json.NewDecoder(resp.Body).Decode(&user)
				require.NoError(t, err)

				if tt.payload.Name != "" {
					assert.Equal(t, tt.payload.Name, user.Name)
				}
				if tt.payload.Email != "" {
					assert.Equal(t, tt.payload.Email, user.Email)
				}
				assert.True(t, user.UpdatedAt.After(user.CreatedAt))
			}
		})
	}
}

func TestHTTP_DeleteUser(t *testing.T) {
	ts, store := setupHTTPServerTest()
	defer ts.Close()

	var testUser User
	for _, u := range store.users {
		testUser = u
		break
	}

	tests := []struct {
		name       string
		userID     string
		wantStatus int
	}{
		{
			name:       "non existent user",
			userID:     "non-existent-id",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "existing user",
			userID:     testUser.ID,
			wantStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(
				http.MethodDelete,
				ts.URL+"/api/users/"+tt.userID,
				nil,
			)
			req.Header.Set("Content-Type", "application/json")
			require.NoError(t, err)

			client := &http.Client{}
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)

			if tt.wantStatus == http.StatusNoContent {
				store.RLock()
				_, exists := store.users[tt.userID]
				store.RUnlock()
				assert.False(t, exists)
			}
		})
	}
}

func TestHTTP_MethodNotAllowed(t *testing.T) {
	ts, _ := setupHTTPServerTest()
	defer ts.Close()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{
			name:       "PATCH not allowed on users",
			method:     http.MethodPatch,
			path:       "/api/users",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "HEAD not allowed on users",
			method:     http.MethodHead,
			path:       "/api/users",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, ts.URL+tt.path, nil)
			require.NoError(t, err)

			client := &http.Client{}
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}
