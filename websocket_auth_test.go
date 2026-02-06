package router

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testClaimsBase struct{}

func (testClaimsBase) Subject() string          { return "" }
func (testClaimsBase) UserID() string           { return "" }
func (testClaimsBase) Role() string             { return "" }
func (testClaimsBase) CanRead(string) bool      { return false }
func (testClaimsBase) CanEdit(string) bool      { return false }
func (testClaimsBase) CanCreate(string) bool    { return false }
func (testClaimsBase) CanDelete(string) bool    { return false }
func (testClaimsBase) HasRole(string) bool      { return false }
func (testClaimsBase) IsAtLeast(string) bool    { return false }

type testClaimsWithID struct {
	testClaimsBase
	tokenID string
}

func (c testClaimsWithID) TokenID() string { return c.tokenID }

type testClaimsNoID struct {
	testClaimsBase
}

type stubWebSocketContext struct {
	*MockContext
	upgradeData map[string]any
}

func newStubWebSocketContext() *stubWebSocketContext {
	return &stubWebSocketContext{
		MockContext: NewMockContext(),
		upgradeData: make(map[string]any),
	}
}

func (s *stubWebSocketContext) Context() context.Context                   { return context.Background() }
func (s *stubWebSocketContext) SetContext(context.Context)                 {}
func (s *stubWebSocketContext) Next() error                                { return nil }
func (s *stubWebSocketContext) IsWebSocket() bool                          { return true }
func (s *stubWebSocketContext) WebSocketUpgrade() error                    { return nil }
func (s *stubWebSocketContext) WriteMessage(int, []byte) error             { return nil }
func (s *stubWebSocketContext) ReadMessage() (int, []byte, error)          { return 0, nil, nil }
func (s *stubWebSocketContext) WriteJSON(any) error                        { return nil }
func (s *stubWebSocketContext) ReadJSON(any) error                         { return nil }
func (s *stubWebSocketContext) WritePing([]byte) error                     { return nil }
func (s *stubWebSocketContext) WritePong([]byte) error                     { return nil }
func (s *stubWebSocketContext) Close() error                               { return nil }
func (s *stubWebSocketContext) CloseWithStatus(int, string) error          { return nil }
func (s *stubWebSocketContext) SetReadDeadline(time.Time) error            { return nil }
func (s *stubWebSocketContext) SetWriteDeadline(time.Time) error           { return nil }
func (s *stubWebSocketContext) SetPingHandler(func([]byte) error)          {}
func (s *stubWebSocketContext) SetPongHandler(func([]byte) error)          {}
func (s *stubWebSocketContext) SetCloseHandler(func(int, string) error)    {}
func (s *stubWebSocketContext) Subprotocol() string                        { return "" }
func (s *stubWebSocketContext) Extensions() []string                       { return nil }
func (s *stubWebSocketContext) RemoteAddr() string                         { return "" }
func (s *stubWebSocketContext) LocalAddr() string                          { return "" }
func (s *stubWebSocketContext) IsConnected() bool                          { return true }
func (s *stubWebSocketContext) ConnectionID() string                       { return "stub-conn" }
func (s *stubWebSocketContext) UpgradeData(key string) (any, bool)         { val, ok := s.upgradeData[key]; return val, ok }

type stubWSClient struct {
	conn WebSocketContext
	ctx  context.Context
}

func (s *stubWSClient) ID() string                                       { return "stub-client" }
func (s *stubWSClient) ConnectionID() string                             { return s.conn.ConnectionID() }
func (s *stubWSClient) Context() context.Context                         { if s.ctx != nil { return s.ctx }; return context.Background() }
func (s *stubWSClient) SetContext(ctx context.Context)                   { s.ctx = ctx }
func (s *stubWSClient) OnMessage(MessageHandler) error                   { return nil }
func (s *stubWSClient) OnJSON(string, JSONHandler) error                 { return nil }
func (s *stubWSClient) Send([]byte) error                                { return nil }
func (s *stubWSClient) SendJSON(any) error                               { return nil }
func (s *stubWSClient) SendWithContext(context.Context, []byte) error    { return nil }
func (s *stubWSClient) SendJSONWithContext(context.Context, any) error   { return nil }
func (s *stubWSClient) Broadcast([]byte) error                           { return nil }
func (s *stubWSClient) BroadcastJSON(any) error                          { return nil }
func (s *stubWSClient) BroadcastWithContext(context.Context, []byte) error {
	return nil
}
func (s *stubWSClient) BroadcastJSONWithContext(context.Context, any) error {
	return nil
}
func (s *stubWSClient) Join(string) error                                { return nil }
func (s *stubWSClient) JoinWithContext(context.Context, string) error    { return nil }
func (s *stubWSClient) Leave(string) error                               { return nil }
func (s *stubWSClient) LeaveWithContext(context.Context, string) error   { return nil }
func (s *stubWSClient) Room(string) RoomBroadcaster                      { return nil }
func (s *stubWSClient) Rooms() []string                                  { return nil }
func (s *stubWSClient) Set(string, any)                                  {}
func (s *stubWSClient) SetWithContext(context.Context, string, any)      {}
func (s *stubWSClient) Get(string) any                                   { return nil }
func (s *stubWSClient) GetString(string) string                          { return "" }
func (s *stubWSClient) GetInt(string) int                                { return 0 }
func (s *stubWSClient) GetBool(string) bool                              { return false }
func (s *stubWSClient) Close(int, string) error                          { return nil }
func (s *stubWSClient) CloseWithContext(context.Context, int, string) error {
	return nil
}
func (s *stubWSClient) IsConnected() bool { return true }
func (s *stubWSClient) Query(key string, defaultValue ...string) string {
	if s.conn == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return ""
	}
	return s.conn.Query(key, defaultValue...)
}
func (s *stubWSClient) Emit(string, any) error                               { return nil }
func (s *stubWSClient) EmitWithContext(context.Context, string, any) error   { return nil }
func (s *stubWSClient) Conn() WebSocketContext                                { return s.conn }

func TestWSTokenIDFromContext(t *testing.T) {
	t.Run("returns-token-id-when-available", func(t *testing.T) {
		claims := testClaimsWithID{tokenID: "token-123"}
		ctx := context.WithValue(context.Background(), WSAuthContextKey{}, claims)

		tokenID, ok := WSTokenIDFromContext(ctx)

		require.True(t, ok)
		require.Equal(t, "token-123", tokenID)
	})

	t.Run("returns-false-when-missing", func(t *testing.T) {
		ctx := context.Background()

		tokenID, ok := WSTokenIDFromContext(ctx)

		require.False(t, ok)
		require.Empty(t, tokenID)
	})

	t.Run("returns-false-when-claims-lack-token-id", func(t *testing.T) {
		claims := testClaimsNoID{}
		ctx := context.WithValue(context.Background(), WSAuthContextKey{}, claims)

		tokenID, ok := WSTokenIDFromContext(ctx)

		require.False(t, ok)
		require.Empty(t, tokenID)
	})
}

func TestDefaultTokenExtractorWithConfig(t *testing.T) {
	t.Run("query-token", func(t *testing.T) {
		wsCtx := newStubWebSocketContext()
		wsCtx.QueriesM["token"] = "query-token"
		client := &stubWSClient{conn: wsCtx}

		extractor := defaultTokenExtractorWithConfig(WSAuthConfig{})
		token, err := extractor(context.Background(), client)

		require.NoError(t, err)
		require.Equal(t, "query-token", token)
	})

	t.Run("query-auth-token", func(t *testing.T) {
		wsCtx := newStubWebSocketContext()
		wsCtx.QueriesM["auth_token"] = "auth-token"
		client := &stubWSClient{conn: wsCtx}

		extractor := defaultTokenExtractorWithConfig(WSAuthConfig{})
		token, err := extractor(context.Background(), client)

		require.NoError(t, err)
		require.Equal(t, "auth-token", token)
	})

	t.Run("authorization-bearer", func(t *testing.T) {
		wsCtx := newStubWebSocketContext()
		wsCtx.HeadersM[HeaderAuthorization] = "Bearer header-token"
		client := &stubWSClient{conn: wsCtx}

		extractor := defaultTokenExtractorWithConfig(WSAuthConfig{})
		token, err := extractor(context.Background(), client)

		require.NoError(t, err)
		require.Equal(t, "header-token", token)
	})

	t.Run("authorization-raw", func(t *testing.T) {
		wsCtx := newStubWebSocketContext()
		wsCtx.HeadersM[HeaderAuthorization] = "raw-token"
		client := &stubWSClient{conn: wsCtx}

		extractor := defaultTokenExtractorWithConfig(WSAuthConfig{})
		token, err := extractor(context.Background(), client)

		require.NoError(t, err)
		require.Equal(t, "raw-token", token)
	})

	t.Run("cookie-enabled", func(t *testing.T) {
		wsCtx := newStubWebSocketContext()
		wsCtx.CookiesM["token"] = "cookie-token"
		client := &stubWSClient{conn: wsCtx}

		extractor := defaultTokenExtractorWithConfig(WSAuthConfig{
			EnableTokenCookie: true,
		})
		token, err := extractor(context.Background(), client)

		require.NoError(t, err)
		require.Equal(t, "cookie-token", token)
	})

	t.Run("cookie-disabled", func(t *testing.T) {
		wsCtx := newStubWebSocketContext()
		wsCtx.CookiesM["token"] = "cookie-token"
		client := &stubWSClient{conn: wsCtx}

		extractor := defaultTokenExtractorWithConfig(WSAuthConfig{})
		token, err := extractor(context.Background(), client)

		require.Error(t, err)
		require.Empty(t, token)
	})
}
