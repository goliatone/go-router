/**
 * GoRouter WebSocket Client Library - Usage Examples
 * 
 * This file contains comprehensive examples for common WebSocket client scenarios
 * using the GoRouter WebSocket Client library.
 */

// Example 1: Simple Chat Client
function createSimpleChatClient() {
    const client = new WebSocketClient('ws://localhost:3000/ws', {
        token: 'your-jwt-token-here',
        autoReconnect: true,
        maxReconnectAttempts: 5,
        heartbeatInterval: 30000,
        debug: true
    });

    // Connection events
    client.on('connected', (event) => {
        console.log('✅ Connected to chat server', event);
        updateUI('Connected', 'success');
    });

    client.on('disconnected', (event) => {
        console.log('❌ Disconnected from chat server', event);
        updateUI('Disconnected', 'error');
    });

    client.on('reconnecting', (event) => {
        console.log(`🔄 Reconnecting... attempt ${event.attempt}/${event.maxAttempts}`);
        updateUI(`Reconnecting (${event.attempt}/${event.maxAttempts})`, 'warning');
    });

    // Authentication events
    client.on('auth_success', (data) => {
        console.log('🔐 Authenticated as:', data.username, `(${data.role})`);
        updateUserInfo(data);
    });

    client.on('auth_failed', (error) => {
        console.error('❌ Authentication failed:', error.message);
        showError('Authentication failed: ' + error.message);
    });

    // Chat message events
    client.on('chat_message', (data) => {
        displayChatMessage({
            username: data.username,
            role: data.role,
            text: data.text,
            timestamp: data.timestamp
        });
    });

    client.on('user_joined', (data) => {
        displaySystemMessage(`${data.username} joined the chat`);
        updateUsersList();
    });

    client.on('user_left', (data) => {
        displaySystemMessage(`${data.username} left the chat`);
        updateUsersList();
    });

    // Send chat message
    function sendMessage(text) {
        if (!client.isAuthenticated()) {
            showError('Please authenticate first');
            return;
        }

        client.send({
            type: 'chat_message',
            text: text
        }).then(() => {
            console.log('Message sent successfully');
            clearMessageInput();
        }).catch(error => {
            console.error('Failed to send message:', error);
            showError('Failed to send message');
        });
    }

    // Get list of connected users
    function getUsersList() {
        return client.sendCommand('get_users', {}, 5000);
    }

    // Connect to server
    function connect() {
        client.connect().then(() => {
            console.log('Connected successfully');
        }).catch(error => {
            console.error('Connection failed:', error);
            showError('Connection failed: ' + error.message);
        });
    }

    // Helper functions (implement according to your UI)
    function updateUI(status, type) { /* Update connection status in UI */ }
    function updateUserInfo(data) { /* Update user info display */ }
    function showError(message) { /* Show error message */ }
    function displayChatMessage(message) { /* Display chat message */ }
    function displaySystemMessage(message) { /* Display system message */ }
    function updateUsersList() { /* Refresh users list */ }
    function clearMessageInput() { /* Clear message input field */ }

    return {
        connect,
        disconnect: () => client.disconnect(),
        sendMessage,
        getUsersList,
        client
    };
}

// Example 2: Admin Dashboard with Commands
function createAdminDashboard() {
    const client = new WebSocketClient('ws://localhost:3000/ws', {
        token: localStorage.getItem('admin_token'),
        autoReconnect: true,
        heartbeatInterval: 30000,
        debug: false,
        logLevel: 'warn'
    });

    let dashboardStats = {
        connectedUsers: 0,
        messagesSent: 0,
        uptime: 0
    };

    // Admin-specific event handling
    client.on('auth_success', (data) => {
        if (data.role !== 'admin') {
            client.disconnect();
            showError('Admin privileges required');
            return;
        }
        
        console.log('👑 Admin authenticated:', data.username);
        initializeDashboard();
    });

    client.on('users_list', (data) => {
        dashboardStats.connectedUsers = data.count;
        updateDashboardStats();
        displayUsersList(data.users);
    });

    client.on('admin_announcement', (data) => {
        displayAdminMessage(data.message);
    });

    // Admin command system
    async function executeAdminCommand(command, params = {}) {
        try {
            const response = await client.sendCommand('admin_command', {
                command: command,
                ...params
            }, 10000); // 10 second timeout for admin commands

            displayCommandResult(command, response);
            return response;
        } catch (error) {
            showError(`Admin command failed: ${error.message}`);
            throw error;
        }
    }

    // Specific admin operations
    async function kickUser(userId) {
        return executeAdminCommand('kick_user', { user_id: userId });
    }

    async function banUser(userId, reason) {
        return executeAdminCommand('ban_user', { 
            user_id: userId, 
            reason: reason 
        });
    }

    async function broadcastMessage(message) {
        return executeAdminCommand('broadcast', { 
            message: message 
        });
    }

    async function getServerStats() {
        return executeAdminCommand('server_stats');
    }

    // Dashboard initialization
    function initializeDashboard() {
        // Start periodic stats updates
        setInterval(async () => {
            try {
                const users = await client.sendCommand('get_users');
                dashboardStats.connectedUsers = users.count;
                
                const metrics = client.getMetrics();
                dashboardStats.messagesSent = metrics.messagessent;
                dashboardStats.uptime = metrics.uptime;
                
                updateDashboardStats();
            } catch (error) {
                console.warn('Failed to update dashboard stats:', error);
            }
        }, 5000);
    }

    // Helper functions
    function updateDashboardStats() { /* Update dashboard statistics */ }
    function displayUsersList(users) { /* Display users in admin panel */ }
    function displayAdminMessage(message) { /* Show admin message */ }
    function displayCommandResult(command, result) { /* Show command result */ }
    function showError(message) { /* Show error message */ }

    return {
        connect: () => client.connect(),
        disconnect: () => client.disconnect(),
        executeAdminCommand,
        kickUser,
        banUser,
        broadcastMessage,
        getServerStats,
        client
    };
}

// Example 3: Real-time Notifications System
function createNotificationClient() {
    const client = new WebSocketClient('ws://localhost:3000/ws', {
        token: getAuthToken(),
        autoReconnect: true,
        maxReconnectAttempts: 10,
        heartbeatInterval: 60000, // Longer interval for notifications
        queueMessages: false, // Don't queue notification subscriptions
        debug: false
    });

    let subscriptions = new Set();
    let notificationQueue = [];

    // Notification event handlers
    client.on('connected', () => {
        // Resubscribe to all notification types
        subscriptions.forEach(type => {
            subscribeToNotifications(type);
        });
    });

    client.on('auth_success', (data) => {
        console.log('📱 Notification client authenticated for:', data.username);
        // Subscribe to default notifications
        subscribeToNotifications('system');
        subscribeToNotifications('user_activity');
    });

    // Handle different notification types
    client.on('system_notification', (data) => {
        showNotification({
            type: 'system',
            title: 'System Notification',
            message: data.message,
            priority: data.priority || 'normal',
            timestamp: data.timestamp
        });
    });

    client.on('user_activity', (data) => {
        showNotification({
            type: 'activity',
            title: 'User Activity',
            message: `${data.username} ${data.action}`,
            priority: 'low',
            timestamp: data.timestamp
        });
    });

    client.on('message', (data) => {
        // Handle custom notification types
        if (data.type.endsWith('_notification')) {
            handleCustomNotification(data);
        }
    });

    // Subscription management
    async function subscribeToNotifications(type) {
        try {
            await client.sendCommand('subscribe_notifications', {
                notification_type: type
            });
            subscriptions.add(type);
            console.log(`📬 Subscribed to ${type} notifications`);
        } catch (error) {
            console.error(`Failed to subscribe to ${type} notifications:`, error);
        }
    }

    async function unsubscribeFromNotifications(type) {
        try {
            await client.sendCommand('unsubscribe_notifications', {
                notification_type: type
            });
            subscriptions.delete(type);
            console.log(`📪 Unsubscribed from ${type} notifications`);
        } catch (error) {
            console.error(`Failed to unsubscribe from ${type} notifications:`, error);
        }
    }

    // Notification display
    function showNotification(notification) {
        // Add to queue if page is not visible
        if (document.hidden) {
            notificationQueue.push(notification);
            return;
        }

        // Show notification based on type and priority
        if (notification.priority === 'high') {
            showHighPriorityNotification(notification);
        } else {
            showRegularNotification(notification);
        }

        // Browser notification API
        if ('Notification' in window && Notification.permission === 'granted') {
            new Notification(notification.title, {
                body: notification.message,
                icon: getNotificationIcon(notification.type),
                tag: notification.type
            });
        }
    }

    function handleCustomNotification(data) {
        // Handle custom notification formats
        showNotification({
            type: data.type.replace('_notification', ''),
            title: data.title || 'Notification',
            message: data.message,
            priority: data.priority || 'normal',
            timestamp: data.timestamp
        });
    }

    // Process queued notifications when page becomes visible
    document.addEventListener('visibilitychange', () => {
        if (!document.hidden && notificationQueue.length > 0) {
            notificationQueue.forEach(showNotification);
            notificationQueue = [];
        }
    });

    // Helper functions
    function getAuthToken() { return localStorage.getItem('auth_token'); }
    function showHighPriorityNotification(notification) { /* Show urgent notification */ }
    function showRegularNotification(notification) { /* Show normal notification */ }
    function getNotificationIcon(type) { return `/icons/${type}.png`; }

    return {
        connect: () => client.connect(),
        disconnect: () => client.disconnect(),
        subscribeToNotifications,
        unsubscribeFromNotifications,
        client
    };
}

// Example 4: Multiplayer Game Client
function createGameClient(gameId) {
    const client = new WebSocketClient(`ws://localhost:3000/game/${gameId}`, {
        token: getGameToken(),
        autoReconnect: true,
        maxReconnectAttempts: 3, // Fewer attempts for games
        reconnectDelay: 2000,
        heartbeatInterval: 15000, // More frequent for games
        debug: true
    });

    let gameState = null;
    let playerId = null;
    let gameCallbacks = {};

    // Game-specific events
    client.on('auth_success', (data) => {
        playerId = data.user_id;
        console.log('🎮 Game client authenticated:', data.username);
        requestGameState();
    });

    client.on('game_state', (data) => {
        gameState = data.state;
        updateGameDisplay(gameState);
    });

    client.on('player_joined', (data) => {
        console.log(`👋 ${data.username} joined the game`);
        addPlayerToGame(data);
    });

    client.on('player_left', (data) => {
        console.log(`👋 ${data.username} left the game`);
        removePlayerFromGame(data.user_id);
    });

    client.on('game_action', (data) => {
        handleGameAction(data);
    });

    client.on('game_over', (data) => {
        handleGameOver(data);
    });

    // Connection handling for games
    client.on('disconnected', (event) => {
        if (gameState && gameState.status === 'active') {
            showGameMessage('Connection lost! Attempting to reconnect...', 'warning');
        }
    });

    client.on('reconnecting', (event) => {
        showGameMessage(`Reconnecting... (${event.attempt}/${event.maxAttempts})`, 'info');
    });

    client.on('connected', () => {
        if (gameState && gameState.status === 'active') {
            showGameMessage('Reconnected! Syncing game state...', 'success');
            requestGameState(); // Resync after reconnection
        }
    });

    // Game actions
    async function performAction(actionType, actionData) {
        try {
            const response = await client.sendCommand('game_action', {
                action: actionType,
                data: actionData,
                player_id: playerId
            }, 3000);

            return response;
        } catch (error) {
            console.error('Game action failed:', error);
            showGameMessage('Action failed: ' + error.message, 'error');
            throw error;
        }
    }

    async function requestGameState() {
        try {
            const response = await client.sendCommand('get_game_state');
            gameState = response.state;
            updateGameDisplay(gameState);
        } catch (error) {
            console.error('Failed to get game state:', error);
        }
    }

    // Game-specific helper functions
    function handleGameAction(data) {
        // Update local game state based on other players' actions
        if (gameCallbacks.onAction) {
            gameCallbacks.onAction(data);
        }
    }

    function handleGameOver(data) {
        gameState.status = 'finished';
        if (gameCallbacks.onGameOver) {
            gameCallbacks.onGameOver(data);
        }
    }

    function updateGameDisplay(state) {
        if (gameCallbacks.onStateUpdate) {
            gameCallbacks.onStateUpdate(state);
        }
    }

    function addPlayerToGame(playerData) {
        if (gameCallbacks.onPlayerJoin) {
            gameCallbacks.onPlayerJoin(playerData);
        }
    }

    function removePlayerFromGame(playerId) {
        if (gameCallbacks.onPlayerLeave) {
            gameCallbacks.onPlayerLeave(playerId);
        }
    }

    function showGameMessage(message, type) {
        if (gameCallbacks.onMessage) {
            gameCallbacks.onMessage(message, type);
        }
    }

    // Helper functions
    function getGameToken() { return localStorage.getItem('game_token'); }

    return {
        connect: () => client.connect(),
        disconnect: () => client.disconnect(),
        performAction,
        requestGameState,
        setCallbacks: (callbacks) => { gameCallbacks = callbacks; },
        getGameState: () => gameState,
        getPlayerId: () => playerId,
        client
    };
}

// Example 5: Monitoring Dashboard
function createMonitoringDashboard() {
    const client = new WebSocketClient('ws://localhost:3000/monitoring', {
        token: getMonitoringToken(),
        autoReconnect: true,
        heartbeatInterval: 10000, // Short interval for monitoring
        queueMessages: false, // Real-time data only
        debug: false
    });

    let metrics = {
        cpu: 0,
        memory: 0,
        connections: 0,
        requestsPerSecond: 0
    };

    let alerts = [];
    let isRecordingData = false;
    let dataBuffer = [];

    // Monitoring events
    client.on('connected', () => {
        console.log('📊 Monitoring dashboard connected');
        subscribeToMetrics();
    });

    client.on('metric_update', (data) => {
        updateMetric(data.metric, data.value, data.timestamp);
    });

    client.on('alert', (data) => {
        handleAlert(data);
    });

    client.on('system_status', (data) => {
        updateSystemStatus(data);
    });

    // Metric subscription
    async function subscribeToMetrics() {
        const metricsToSubscribe = ['cpu', 'memory', 'connections', 'rps'];
        
        for (const metric of metricsToSubscribe) {
            try {
                await client.sendCommand('subscribe_metric', {
                    metric: metric,
                    interval: 1000 // 1 second updates
                });
                console.log(`📈 Subscribed to ${metric} metrics`);
            } catch (error) {
                console.error(`Failed to subscribe to ${metric}:`, error);
            }
        }
    }

    // Metric handling
    function updateMetric(metricName, value, timestamp) {
        metrics[metricName] = value;
        
        // Update dashboard display
        updateMetricDisplay(metricName, value);
        
        // Record data if recording
        if (isRecordingData) {
            dataBuffer.push({
                metric: metricName,
                value: value,
                timestamp: timestamp
            });
        }

        // Check for alerts
        checkMetricThresholds(metricName, value);
    }

    function checkMetricThresholds(metric, value) {
        const thresholds = {
            cpu: 80,
            memory: 90,
            connections: 1000,
            requestsPerSecond: 100
        };

        if (thresholds[metric] && value > thresholds[metric]) {
            createAlert({
                type: 'threshold',
                metric: metric,
                value: value,
                threshold: thresholds[metric],
                severity: 'warning',
                timestamp: new Date().toISOString()
            });
        }
    }

    function handleAlert(alertData) {
        alerts.unshift(alertData);
        
        // Keep only last 100 alerts
        if (alerts.length > 100) {
            alerts = alerts.slice(0, 100);
        }

        displayAlert(alertData);
        
        // Send notification if severe
        if (alertData.severity === 'critical') {
            sendNotification(alertData);
        }
    }

    // Data recording
    function startRecording() {
        isRecordingData = true;
        dataBuffer = [];
        console.log('📝 Started recording metrics data');
    }

    function stopRecording() {
        isRecordingData = false;
        console.log('⏹ Stopped recording metrics data');
        return dataBuffer.slice();
    }

    // Export data
    function exportData(format = 'json') {
        const data = stopRecording();
        
        if (format === 'csv') {
            return convertToCSV(data);
        }
        
        return JSON.stringify(data, null, 2);
    }

    // Helper functions
    function getMonitoringToken() { return localStorage.getItem('monitoring_token'); }
    function updateMetricDisplay(metric, value) { /* Update dashboard chart/gauge */ }
    function updateSystemStatus(status) { /* Update system status indicator */ }
    function displayAlert(alert) { /* Show alert in dashboard */ }
    function createAlert(alertData) { /* Create and display new alert */ }
    function sendNotification(alert) { /* Send push/email notification */ }
    function convertToCSV(data) { /* Convert JSON data to CSV */ }

    return {
        connect: () => client.connect(),
        disconnect: () => client.disconnect(),
        subscribeToMetrics,
        startRecording,
        stopRecording,
        exportData,
        getMetrics: () => metrics,
        getAlerts: () => alerts,
        client
    };
}

// Export examples for use in different environments
if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        createSimpleChatClient,
        createAdminDashboard,
        createNotificationClient,
        createGameClient,
        createMonitoringDashboard
    };
}

// Browser globals
if (typeof window !== 'undefined') {
    window.WebSocketClientExamples = {
        createSimpleChatClient,
        createAdminDashboard,
        createNotificationClient,
        createGameClient,
        createMonitoringDashboard
    };
}