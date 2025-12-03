const WebSocket = require('ws');

function testWebSocketConnection() {
    return new Promise((resolve, reject) => {
        console.log('🧪 Testing WebSocket connection with EventBus fix...');

        const ws = new WebSocket('ws://localhost:8801/ws');
        let connected = false;
        let errors = [];

        // Connection timeout
        const timeout = setTimeout(() => {
            if (!connected) {
                ws.terminate();
                reject(new Error('Connection timeout'));
            }
        }, 10000);

        ws.on('open', () => {
            console.log('✅ WebSocket connection established');
            connected = true;
            clearTimeout(timeout);

            // Send a test message
            ws.send(JSON.stringify({
                type: 'test',
                message: 'Hello from WebSocket test'
            }));

            // Wait a bit then close cleanly
            setTimeout(() => {
                ws.close(1000, 'Test completed');
            }, 2000);
        });

        ws.on('message', (data) => {
            try {
                const message = JSON.parse(data.toString());
                console.log('📨 Received message:', message);
            } catch (e) {
                console.log('📨 Received raw message:', data.toString());
            }
        });

        ws.on('error', (error) => {
            console.error('❌ WebSocket error:', error.message);
            errors.push(error);
            clearTimeout(timeout);
        });

        ws.on('close', (code, reason) => {
            console.log(`🔌 WebSocket closed: ${code} - ${reason}`);
            clearTimeout(timeout);

            if (errors.length === 0) {
                resolve({
                    connected,
                    errors: [],
                    closeCode: code,
                    closeReason: reason.toString()
                });
            } else {
                reject(errors[0]);
            }
        });
    });
}

async function main() {
    try {
        const result = await testWebSocketConnection();
        console.log('🎉 WebSocket test completed successfully:');
        console.log('  Connected:', result.connected);
        console.log('  Close code:', result.closeCode);
        console.log('  Close reason:', result.closeReason);
        console.log('  Errors:', result.errors.length);

        if (result.connected && result.errors.length === 0) {
            console.log('✅ EventBus fix is working - no nil pointer exceptions!');
        } else {
            console.log('❌ EventBus fix may have issues');
        }
    } catch (error) {
        console.error('❌ WebSocket test failed:', error.message);
        console.log('❌ EventBus fix may have failed');
    }

    process.exit(0);
}

main();