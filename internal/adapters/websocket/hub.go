package websocket

import (
	"encoding/json"
	"errors"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	realtimeuc "github.com/bemulima/ms-go-dialog/internal/usecase/realtime"
	"github.com/google/uuid"
	"sync"
	"time"
)

var errConnectionLimit = errors.New("realtime connection limit reached")

type Hub struct {
	mu                    sync.RWMutex
	dialogs               map[uuid.UUID]map[*client]struct{}
	users                 map[uuid.UUID]map[*client]struct{}
	userCounts            map[uuid.UUID]int
	maxConnectionsPerUser int
	queueSize             int
	closed                bool
}
type client struct {
	session   realtimeuc.Session
	send      chan []byte
	done      chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	active    bool
	pending   [][]byte
	dialogs   map[uuid.UUID]struct{}
	queueSize int
}

func NewHub(maxConnectionsPerUser, queueSize int) *Hub {
	if maxConnectionsPerUser < 1 {
		maxConnectionsPerUser = 5
	}
	if queueSize < 1 {
		queueSize = 64
	}
	return &Hub{dialogs: make(map[uuid.UUID]map[*client]struct{}), users: make(map[uuid.UUID]map[*client]struct{}), userCounts: make(map[uuid.UUID]int), maxConnectionsPerUser: maxConnectionsPerUser, queueSize: queueSize}
}
func (h *Hub) register(session realtimeuc.Session) (*client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if session.UserID == uuid.Nil || session.SpaceID == uuid.Nil {
		return nil, errors.New("invalid realtime session")
	}
	if h.closed {
		return nil, errors.New("realtime hub is closed")
	}
	if h.userCounts[session.UserID] >= h.maxConnectionsPerUser {
		return nil, errConnectionLimit
	}
	item := &client{session: session, send: make(chan []byte, h.queueSize), done: make(chan struct{}), pending: make([][]byte, 0), dialogs: make(map[uuid.UUID]struct{}), queueSize: h.queueSize}
	if h.users[session.UserID] == nil {
		h.users[session.UserID] = make(map[*client]struct{})
	}
	h.users[session.UserID][item] = struct{}{}
	for dialogID := range session.DialogSequences {
		h.addDialogLocked(item, dialogID)
	}
	h.userCounts[session.UserID]++
	return item, nil
}
func (h *Hub) addDialogLocked(item *client, dialogID uuid.UUID) {
	item.mu.Lock()
	defer item.mu.Unlock()
	if h.dialogs[dialogID] == nil {
		h.dialogs[dialogID] = make(map[*client]struct{})
	}
	h.dialogs[dialogID][item] = struct{}{}
	item.dialogs[dialogID] = struct{}{}
}
func (h *Hub) removeDialogLocked(item *client, dialogID uuid.UUID) {
	item.mu.Lock()
	defer item.mu.Unlock()
	delete(item.dialogs, dialogID)
	if clients := h.dialogs[dialogID]; clients != nil {
		delete(clients, item)
		if len(clients) == 0 {
			delete(h.dialogs, dialogID)
		}
	}
}
func (h *Hub) unregister(item *client) {
	if item == nil {
		return
	}
	h.mu.Lock()
	if clients := h.users[item.session.UserID]; clients != nil {
		if _, ok := clients[item]; ok {
			delete(clients, item)
			for dialogID := range item.dialogs {
				h.removeDialogLocked(item, dialogID)
			}
			h.userCounts[item.session.UserID]--
			if len(clients) == 0 {
				delete(h.users, item.session.UserID)
			}
			if h.userCounts[item.session.UserID] <= 0 {
				delete(h.userCounts, item.session.UserID)
			}
		}
	}
	h.mu.Unlock()
	item.closeOnce.Do(func() { close(item.done) })
}
func (h *Hub) BroadcastLifecycle(subject domain.EventSubject, payload []byte) {
	route, envelope, err := lifecycleEnvelope(subject, payload)
	if err != nil {
		return
	}
	h.mu.Lock()
	switch subject {
	case domain.EventDialogCreated:
		for _, userID := range route.ParticipantIDs {
			for item := range h.users[userID] {
				if item.session.SpaceID == route.SpaceID {
					h.addDialogLocked(item, route.DialogID)
				}
			}
		}
	case domain.EventDialogMemberAdded:
		for item := range h.users[route.UserID] {
			if item.session.SpaceID == route.SpaceID {
				h.addDialogLocked(item, route.DialogID)
			}
		}
	}
	slow := make([]*client, 0)
	for item := range h.dialogs[route.DialogID] {
		if !item.push(envelope) {
			slow = append(slow, item)
		}
	}
	if subject == domain.EventDialogMemberRemoved {
		for item := range h.users[route.UserID] {
			h.removeDialogLocked(item, route.DialogID)
		}
	}
	h.mu.Unlock()
	for _, item := range slow {
		h.unregister(item)
	}
}
func (h *Hub) BroadcastEphemeral(payload []byte) {
	var event struct {
		V          int       `json:"v"`
		Type       string    `json:"type"`
		EventID    uuid.UUID `json:"event_id"`
		DialogID   uuid.UUID `json:"dialog_id"`
		OccurredAt time.Time `json:"occurred_at"`
		Data       struct {
			UserID uuid.UUID `json:"user_id"`
		} `json:"data"`
	}
	if json.Unmarshal(payload, &event) != nil || event.V != protocolVersion || (event.Type != "typing.started" && event.Type != "typing.stopped") || event.EventID == uuid.Nil || event.DialogID == uuid.Nil || event.Data.UserID == uuid.Nil || event.OccurredAt.IsZero() {
		return
	}
	h.broadcast(event.DialogID, append([]byte(nil), payload...))
}
func (h *Hub) broadcast(dialogID uuid.UUID, payload []byte) {
	h.mu.RLock()
	slow := make([]*client, 0)
	for item := range h.dialogs[dialogID] {
		if !item.push(payload) {
			slow = append(slow, item)
		}
	}
	h.mu.RUnlock()
	for _, item := range slow {
		h.unregister(item)
	}
}
func (h *Hub) activate(item *client, initial ...[]byte) [][]byte {
	item.mu.Lock()
	defer item.mu.Unlock()
	result := append(make([][]byte, 0, len(initial)+len(item.pending)), initial...)
	result = append(result, item.pending...)
	item.pending = nil
	item.active = true
	return result
}
func (c *client) push(payload []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		if len(c.pending) >= c.queueSize {
			return false
		}
		c.pending = append(c.pending, payload)
		return true
	}
	select {
	case c.send <- payload:
		return true
	default:
		return false
	}
}
func (c *client) hasDialog(id uuid.UUID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.dialogs[id]
	return ok
}
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	items := make([]*client, 0)
	for _, clients := range h.users {
		for item := range clients {
			items = append(items, item)
		}
	}
	h.dialogs = make(map[uuid.UUID]map[*client]struct{})
	h.users = make(map[uuid.UUID]map[*client]struct{})
	h.userCounts = make(map[uuid.UUID]int)
	h.mu.Unlock()
	for _, item := range items {
		item.closeOnce.Do(func() { close(item.done) })
	}
}
