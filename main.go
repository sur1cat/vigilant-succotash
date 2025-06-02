package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"server/internal/protocol"
	"sync"
	_ "time"
)

var (
	conn net.Conn
	mu   sync.Mutex
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

		mu.Lock()
		conn = c
		mu.Unlock()

		go handleConnection(c)
	}
}

func handleConnection(c net.Conn) {
	defer c.Close()
	buf := make([]byte, 1024)
	for {
		n, err := c.Read(buf)
		if err != nil {
			log.Println("Connection closed")
			return
		}
		log.Printf("Received: %x\n", buf[:n])

		resp := protocol.HandleIncoming(buf[:n])
		if resp != nil {
			c.Write(resp)
		}
	}
}

func handleSendCommand(w http.ResponseWriter, r *http.Request) {
	cmd := r.URL.Query().Get("cmd")
	token := r.URL.Query().Get("token")
	slot := r.URL.Query().Get("slot")

	if conn == nil {
		http.Error(w, "No device connected", http.StatusBadRequest)
		return
	}
	payload := protocol.CreateCommand(cmd, token, slot)
	if payload == nil {
		http.Error(w, "Invalid command or parameters", http.StatusBadRequest)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	conn.Write(payload)
	fmt.Fprintf(w, "Sent: %x", payload)
}

func Pong(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("pong"))
}
