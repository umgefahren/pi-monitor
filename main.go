package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// How often we check statuses
	checkInterval = 2 * time.Second
	// Connection timeout for checking port 22
	dialTimeout = 1 * time.Second
)

// A single Pi’s connectivity status
type PiStatus struct {
	Host   string `json:"host"`
	Status string `json:"status"`
}

var (
	// We’ll track each Pi’s status in a map
	piStatuses = make(map[string]string)
	// Mutex to guard read/write to piStatuses
	statusMutex sync.Mutex
	// List of Pi hostnames
	piHosts = []string{}
	// WebSocket upgrader
	upgrader = websocket.Upgrader{}
	// Slice of currently connected WebSocket clients
	websocketClients = make(map[*websocket.Conn]bool)
	// Mutex to guard read/write to websocketClients
	clientsMutex sync.Mutex
)

// checkPiStatuses runs periodically to update the connectivity info
func checkPiStatuses() {
	for {
		updatedStatuses := make(map[string]string)

		for _, host := range piHosts {
			address := host + ":22"
			conn, err := net.DialTimeout("tcp", address, dialTimeout)
			if err != nil {
				updatedStatuses[host] = "DOWN"
			} else {
				updatedStatuses[host] = "UP"
				conn.Close()
			}
		}

		// Update the global piStatuses map
		statusMutex.Lock()
		for host, status := range updatedStatuses {
			piStatuses[host] = status
		}
		statusMutex.Unlock()

		// Broadcast new statuses
		broadcastStatus()

		// Sleep until next check
		time.Sleep(checkInterval)
	}
}

// broadcastStatus sends the current statuses to all WebSocket clients
func broadcastStatus() {
	// Grab a snapshot of the statuses
	statusMutex.Lock()
	statuses := make([]PiStatus, 0, len(piHosts))
	for _, host := range piHosts {
		statuses = append(statuses, PiStatus{
			Host:   host,
			Status: piStatuses[host],
		})
	}
	statusMutex.Unlock()

	// Prepare data to send via WebSocket
	// You can also use JSON encoding if you prefer
	msg := strings.Builder{}
	msg.WriteString(`{"type":"update","data":[`)
	for i, s := range statuses {
		msg.WriteString(fmt.Sprintf(`{"host":"%s","status":"%s"}`, s.Host, s.Status))
		if i < len(statuses)-1 {
			msg.WriteString(",")
		}
	}
	msg.WriteString(`]}`)

	// Send to all connected clients
	clientsMutex.Lock()
	defer clientsMutex.Unlock()

	for client := range websocketClients {
		err := client.WriteMessage(websocket.TextMessage, []byte(msg.String()))
		if err != nil {
			log.Printf("error writing message to client: %v", err)
			// If there's an error, assume client is disconnected
			client.Close()
			delete(websocketClients, client)
		}
	}
}

func main() {
	piHosts = strings.Split(os.Getenv("PI_HOSTS"), ",")

	// Initialize statuses to "UNKNOWN" at startup
	for _, host := range piHosts {
		piStatuses[host] = "UNKNOWN"
	}

	// Start the goroutine that checks statuses in the background
	go checkPiStatuses()

	// Serve the frontend
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/index.html")
	})

	// WebSocket endpoint
	http.HandleFunc("/ws", handleWebSocket)

	// Start the server
	fmt.Println("Server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// handleWebSocket upgrades an HTTP connection to a WebSocket
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}

	clientsMutex.Lock()
	websocketClients[conn] = true
	clientsMutex.Unlock()

	// Optionally, send initial statuses right after connection
	broadcastStatus()

	// Keep the connection alive; if you want to read from client, do so here
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			clientsMutex.Lock()
			conn.Close()
			delete(websocketClients, conn)
			clientsMutex.Unlock()
			break
		}
	}
}
