// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package websocket

import (
	"sync"

	"code.vikunja.io/api/pkg/log"
)

// Hub maintains the set of active connections and delivers messages to them.
type Hub struct {
	mu          sync.RWMutex
	connections map[int64][]*Connection            // userID -> connections
	projects    map[int64]map[*Connection]struct{} // projectID -> subscribed connections
}

// NewHub creates a new Hub.
func NewHub() *Hub {
	return &Hub{
		connections: make(map[int64][]*Connection),
		projects:    make(map[int64]map[*Connection]struct{}),
	}
}

// Register adds a connection to the hub.
func (h *Hub) Register(conn *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.connections[conn.userID] = append(h.connections[conn.userID], conn)
	log.Debugf("WebSocket: registered connection for user %d (total: %d)", conn.userID, len(h.connections[conn.userID]))
}

// Unregister removes a connection from the hub, including its project subscriptions.
func (h *Hub) Unregister(conn *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.connections[conn.userID]
	for i, c := range conns {
		if c == conn {
			h.connections[conn.userID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	remaining := len(h.connections[conn.userID])
	if remaining == 0 {
		delete(h.connections, conn.userID)
	}
	for _, projectID := range conn.SubscribedProjects() {
		h.unsubscribeProjectLocked(conn, projectID)
	}
	log.Debugf("WebSocket: unregistered connection for user %d (remaining: %d)", conn.userID, remaining)
}

// SubscribeProject indexes conn under projectID so PublishForProject reaches it.
func (h *Hub) SubscribeProject(conn *Connection, projectID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	conn.subscribeProject(projectID)
	if h.projects[projectID] == nil {
		h.projects[projectID] = make(map[*Connection]struct{})
	}
	h.projects[projectID][conn] = struct{}{}
}

// UnsubscribeProject drops conn from the projectID index.
func (h *Hub) UnsubscribeProject(conn *Connection, projectID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unsubscribeProjectLocked(conn, projectID)
}

func (h *Hub) unsubscribeProjectLocked(conn *Connection, projectID int64) {
	conn.unsubscribeProject(projectID)
	conns := h.projects[projectID]
	delete(conns, conn)
	if len(conns) == 0 {
		delete(h.projects, projectID)
	}
}

func (h *Hub) DisconnectSession(userID int64, sessionID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, conn := range h.connections[userID] {
		if conn.SessionID() == sessionID {
			conn.close("session revoked")
		}
	}
}

func (h *Hub) DisconnectUser(userID int64) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, conn := range h.connections[userID] {
		conn.close("session revoked")
	}
}

// PublishForUser sends an event to all connections of a specific user that are subscribed to the given event.
func (h *Hub) PublishForUser(userID int64, event string, data any) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	conns := h.connections[userID]
	msg := OutgoingMessage{
		Event: event,
		Data:  data,
	}

	for _, conn := range conns {
		if !conn.IsSubscribed(event) {
			continue
		}
		conn.trySend(msg)
	}
}

// PublishForProjects sends one event to every connection subscribed to any of
// the given projects. allow is asked once per user and project, outside the
// hub lock; a subscription it rejects is dropped and the connection is told so
// with an unsubscribed action, so a user who lost access stops receiving
// events. A connection subscribed to several of the projects gets the message
// once.
func (h *Hub) PublishForProjects(projectIDs []int64, event string, data any, allow func(userID, projectID int64) bool) {
	type subscription struct {
		conn      *Connection
		projectID int64
	}
	var subscriptions []subscription
	h.mu.RLock()
	for _, projectID := range projectIDs {
		for conn := range h.projects[projectID] {
			subscriptions = append(subscriptions, subscription{conn: conn, projectID: projectID})
		}
	}
	h.mu.RUnlock()

	type userProject struct{ userID, projectID int64 }
	verdicts := make(map[userProject]bool)
	recipients := make(map[*Connection]struct{})
	for _, sub := range subscriptions {
		key := userProject{userID: sub.conn.UserID(), projectID: sub.projectID}
		allowed, decided := verdicts[key]
		if !decided {
			allowed = allow == nil || allow(key.userID, key.projectID)
			verdicts[key] = allowed
		}
		if allowed {
			recipients[sub.conn] = struct{}{}
			continue
		}
		h.UnsubscribeProject(sub.conn, sub.projectID)
		sub.conn.trySend(OutgoingMessage{
			Action:    ActionUnsubscribed,
			Event:     EventProject,
			ProjectID: sub.projectID,
			Reason:    ReasonForbidden,
		})
		log.Debugf("WebSocket: dropped project %d subscription of user %d: %s", sub.projectID, key.userID, ReasonForbidden)
	}

	msg := OutgoingMessage{
		Event: event,
		Data:  data,
	}
	for conn := range recipients {
		conn.trySend(msg)
	}
}

// PublishForProject is PublishForProjects for a single project.
func (h *Hub) PublishForProject(projectID int64, event string, data any, allow func(userID, projectID int64) bool) {
	h.PublishForProjects([]int64{projectID}, event, data, allow)
}
