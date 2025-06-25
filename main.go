package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"server/internal/protocol"
	"sync"
)

type StationInfo struct {
	Conn      net.Conn
	Token     string
	StationID string
}

var (
	stations = make(map[string]*StationInfo) // Хранит информацию о станциях по StationID
	mu       sync.RWMutex
)

func main() {
	go startTCPServer()

	http.HandleFunc("/send", handleSendCommand)
	http.HandleFunc("/stations", handleListStations)
	http.HandleFunc("/ping", Pong)

	log.Println("HTTP server listening on :8080")
	log.Println("Available endpoints:")
	log.Println("  GET /stations - list all connected stations with tokens")
	log.Println("  GET /send?stationID=<id>&cmd=<command>&slot=<slot> - send command (token auto-selected)")
	log.Println("  GET /ping - health check")

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
		for id, station := range stations {
			if station.Conn == c {
				delete(stations, id)
				log.Printf("Station %s disconnected", id)
			}
		}
		mu.Unlock()
	}()

	buf := make([]byte, 1024)
	var currentStation *StationInfo

	for {
		n, err := c.Read(buf)
		if err != nil {
			log.Printf("Connection error: %v", err)
			return
		}
		log.Printf("Received from %s: %x", getStationID(c), buf[:n])

		resp, stationID := protocol.HandleIncoming(buf[:n])

		// Если получили Station ID (это login), сохраняем информацию о станции
		if stationID != "" && currentStation == nil {
			// Извлекаем токен из пакета логина
			if len(buf) >= 9 {
				token := hex.EncodeToString(buf[5:9])

				currentStation = &StationInfo{
					Conn:      c,
					Token:     token,
					StationID: stationID,
				}

				mu.Lock()
				stations[stationID] = currentStation
				mu.Unlock()

				log.Printf("Station registered: ID=%s, Token=%s", stationID, token)
			}
		}

		if resp != nil {
			_, err := c.Write(resp)
			if err != nil {
				log.Printf("Write error: %v", err)
				return
			}
			log.Printf("Sent response to %s: %x", getStationID(c), resp)
		}
	}
}

func getStationID(conn net.Conn) string {
	mu.RLock()
	defer mu.RUnlock()
	for id, station := range stations {
		if station.Conn == conn {
			return id
		}
	}
	return "unknown"
}

func handleSendCommand(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("stationID")
	cmd := r.URL.Query().Get("cmd")
	slot := r.URL.Query().Get("slot")

	// Позволяем пользователю переопределить токен, если нужно
	tokenParam := r.URL.Query().Get("token")

	mu.RLock()
	station, exists := stations[stationID]
	mu.RUnlock()

	if !exists {
		http.Error(w, fmt.Sprintf("No station connected with ID: %s", stationID), http.StatusBadRequest)
		return
	}

	// Используем токен из параметра или сохраненный токен станции
	token := tokenParam
	if token == "" {
		token = station.Token
	}

	if token == "" {
		http.Error(w, "No token available for this station", http.StatusBadRequest)
		return
	}

	payload := protocol.CreateCommand(cmd, token, slot)
	if payload == nil {
		http.Error(w, "Invalid command or parameters", http.StatusBadRequest)
		return
	}

	_, err := station.Conn.Write(payload)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to send command: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"status":    "success",
		"stationID": stationID,
		"command":   cmd,
		"token":     token,
		"payload":   hex.EncodeToString(payload),
		"slot":      slot,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleListStations(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	stationList := make([]map[string]string, 0, len(stations))
	for id, station := range stations {
		stationList = append(stationList, map[string]string{
			"stationID": id,
			"token":     station.Token,
			"status":    "connected",
		})
	}
	mu.RUnlock()

	response := map[string]interface{}{
		"count":    len(stationList),
		"stations": stationList,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func Pong(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("pong"))
}
