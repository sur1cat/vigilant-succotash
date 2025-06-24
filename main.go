package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"server/internal/protocol"
	"sync"
)

var (
	connections = make(map[string]net.Conn) // Хранит соединения по StationID
	mu          sync.RWMutex
)

func main() {
	go startTCPServer()

	http.HandleFunc("/send", handleSendCommand)
	http.HandleFunc("/ping", Pong)
	log.Println("HTTP listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func startTCPServer() {
	listener, err := net.Listen("tcp", ":9000")
	if err != nil {
		log.Fatalf("Failed to listen on TCP port: %v", err)
	}
	defer listener.Close()
	log.Println("TCP server listening on :9000")

	for {
		c, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		log.Println("New station connected")
		go handleConnection(c)
	}
}

func handleConnection(c net.Conn) {
	defer func() {
		c.Close()
		mu.Lock()
		for id, conn := range connections {
			if conn == c {
				delete(connections, id)
				log.Printf("Station %s disconnected", id)
			}
		}
		mu.Unlock()
	}()

	buf := make([]byte, 1024)
	var stationID string
	for {
		n, err := c.Read(buf)
		if err != nil {
			log.Printf("Connection error: %v", err)
			return
		}
		log.Printf("Received: %x\n", buf[:n])

		resp, id := protocol.HandleIncoming(buf[:n])
		if id != "" && stationID == "" {
			stationID = id
			mu.Lock()
			connections[stationID] = c
			mu.Unlock()
			log.Printf("Station registered with ID: %s", stationID)
		}
		if resp != nil {
			_, err := c.Write(resp)
			if err != nil {
				log.Printf("Write error: %v", err)
				return
			}
			log.Printf("Sent response: %x", resp)
		}
	}
}

func handleSendCommand(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("stationID")
	cmd := r.URL.Query().Get("cmd")
	token := r.URL.Query().Get("token")
	slot := r.URL.Query().Get("slot")

	mu.RLock()
	conn, exists := connections[stationID]
	mu.RUnlock()

	if !exists {
		http.Error(w, fmt.Sprintf("No device connected with ID %s", stationID), http.StatusBadRequest)
		return
	}

	payload := protocol.CreateCommand(cmd, token, slot)
	if payload == nil {
		http.Error(w, "Invalid command or parameters", http.StatusBadRequest)
		return
	}

	_, err := conn.Write(payload)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to send command: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Sent to %s: %x", stationID, payload)
}

func Pong(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("pong"))
}
