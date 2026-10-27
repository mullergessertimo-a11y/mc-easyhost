package main

import (
	"encoding/json"
	"sync"
	"time"
)

// Hub fans out status changes and console lines to every open UI window
// over Server-Sent Events, and keeps a backlog for windows that open later.
type Hub struct {
	mu        sync.Mutex
	clients   map[chan []byte]bool
	backlog   []string
	lastSeen  time.Time // last moment at least one window was connected
	hadClient bool
}

const backlogSize = 2000

func NewHub() *Hub { return &Hub{clients: map[chan []byte]bool{}, lastSeen: time.Now()} }

func event(name string, data any) []byte {
	b, _ := json.Marshal(data)
	return []byte("event: " + name + "\ndata: " + string(b) + "\n\n")
}

func (h *Hub) broadcast(msg []byte) {
	for c := range h.clients {
		select {
		case c <- msg:
		default: // slow client; drop rather than block the server
		}
	}
}

func (h *Hub) SendStatus(s Status) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.broadcast(event("status", s))
}

func (h *Hub) SendLog(line string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.backlog = append(h.backlog, line)
	if len(h.backlog) > backlogSize {
		h.backlog = h.backlog[len(h.backlog)-backlogSize:]
	}
	h.broadcast(event("log", line))
}

func (h *Hub) Subscribe() (chan []byte, []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := make(chan []byte, 512)
	h.clients[c] = true
	h.hadClient = true
	return c, append([]string(nil), h.backlog...)
}

func (h *Hub) Unsubscribe(c chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
	h.lastSeen = time.Now()
}

// IdleFor reports how long no window has been connected (0 if one is).
func (h *Hub) IdleFor() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) > 0 {
		return 0
	}
	return time.Since(h.lastSeen)
}

func (h *Hub) HadClient() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hadClient
}
