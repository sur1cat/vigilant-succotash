package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"server/internal/protocol"
	"strings"
	"sync"
)

var (
	connections = make(map[string]net.Conn) // Хранит соединения по StationID
	mu          sync.RWMutex
)

type SendCommandRequest struct {
	StationID string `json:"station_id"`
	Cmd       string `json:"cmd"`
	Token     string `json:"token"`
	Slot      string `json:"slot,omitempty"`
}

type StationInfo struct {
	StationID string `json:"stationID"`
	Status    string `json:"status"`
	Token     string `json:"token"`
}

type StationsResponse struct {
	Count    int           `json:"count"`
	Stations []StationInfo `json:"stations"`
}

func main() {
	go startTCPServer()

	http.HandleFunc("/send", handleSendCommand)
	http.HandleFunc("/stations", handleListStations)
	http.HandleFunc("/ping", handlePong)

	log.Println("HTTP server listening on :8080")
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
				break
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
		log.Printf("Received from station: %x", buf[:n])

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
			log.Printf("Sent response to %s: %x", stationID, resp)
		}
	}
}

func handleSendCommand(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req SendCommandRequest
	var stationID, cmd, token, slot string

	// Поддерживаем как JSON, так и URL параметры
	if r.Header.Get("Content-Type") == "application/json" || strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error reading request body: %v", err), http.StatusBadRequest)
			return
		}

		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, fmt.Sprintf("Error parsing JSON: %v", err), http.StatusBadRequest)
			return
		}

		stationID = req.StationID
		cmd = req.Cmd
		token = req.Token
		slot = req.Slot
	} else {
		// URL параметры (поддерживаем оба варианта названий)
		stationID = r.URL.Query().Get("stationID")
		if stationID == "" {
			stationID = r.URL.Query().Get("station_id")
		}
		cmd = r.URL.Query().Get("cmd")
		token = r.URL.Query().Get("token")
		slot = r.URL.Query().Get("slot")
	}

	log.Printf("Send command request: stationID=%s, cmd=%s, token=%s, slot=%s", stationID, cmd, token, slot)

	if stationID == "" || cmd == "" || token == "" {
		http.Error(w, "Missing required parameters: station_id/stationID, cmd, token", http.StatusBadRequest)
		return
	}

	mu.RLock()
	conn, exists := connections[stationID]
	mu.RUnlock()

	if !exists {
		log.Printf("Station %s not found in connections. Available stations: %v", stationID, getConnectedStationIDs())
		http.Error(w, fmt.Sprintf("No station connected with ID: %s", stationID), http.StatusBadRequest)
		return
	}

	payload := protocol.CreateCommand(cmd, token, slot)
	if payload == nil {
		http.Error(w, "Invalid command or parameters", http.StatusBadRequest)
		return
	}

	log.Printf("Sending command to station %s: %x", stationID, payload)
	_, err := conn.Write(payload)
	if err != nil {
		log.Printf("Failed to send command to station %s: %v", stationID, err)
		http.Error(w, fmt.Sprintf("Failed to send command: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"status":    "success",
		"message":   fmt.Sprintf("Command sent to station %s", stationID),
		"stationID": stationID,
		"command":   cmd,
		"payload":   fmt.Sprintf("%x", payload),
	}

	json.NewEncoder(w).Encode(response)
}

func handleListStations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	mu.RLock()
	stations := make([]StationInfo, 0, len(connections))
	for stationID := range connections {
		stations = append(stations, StationInfo{
			StationID: stationID,
			Status:    "connected",
			Token:     "11223344", // Можно хранить реальные токены если нужно
		})
	}
	mu.RUnlock()

	response := StationsResponse{
		Count:    len(stations),
		Stations: stations,
	}

	json.NewEncoder(w).Encode(response)
}

func handlePong(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("pong"))
}

func getConnectedStationIDs() []string {
	mu.RLock()
	defer mu.RUnlock()

	ids := make([]string, 0, len(connections))
	for id := range connections {
		ids = append(ids, id)
	}
	return ids
}
