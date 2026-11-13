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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHubRegisterUnregister(t *testing.T) {
	h := NewHub()
	conn := &Connection{
		userID:        1,
		subscriptions: make(map[string]bool),
		send:          make(chan OutgoingMessage, 16),
	}
	h.Register(conn)
	assert.Len(t, h.connections[1], 1)

	h.Unregister(conn)
	assert.Empty(t, h.connections[1])
	_, exists := h.connections[1]
	assert.False(t, exists, "map entry should be deleted when last connection is removed")
}

func TestHubPublishToSubscribedConnection(t *testing.T) {
	h := NewHub()
	conn := &Connection{
		userID:        1,
		subscriptions: make(map[string]bool),
		send:          make(chan OutgoingMessage, 16),
	}
	h.Register(conn)
	conn.subscriptions["notification.created"] = true

	h.PublishForUser(1, "notification.created", map[string]string{"id": "1"})

	msg := <-conn.send
	assert.Equal(t, "notification.created", msg.Event)
}

func TestHubPublishSkipsUnsubscribedConnection(t *testing.T) {
	h := NewHub()
	conn := &Connection{
		userID:        1,
		subscriptions: make(map[string]bool),
		send:          make(chan OutgoingMessage, 16),
	}
	h.Register(conn)
	// Not subscribed to "notification.created"

	h.PublishForUser(1, "notification.created", map[string]string{"id": "1"})

	assert.Empty(t, conn.send)
}

func TestHubPublishSkipsOtherUsers(t *testing.T) {
	h := NewHub()
	conn := &Connection{
		userID:        2,
		subscriptions: make(map[string]bool),
		send:          make(chan OutgoingMessage, 16),
	}
	h.Register(conn)
	conn.subscriptions["notification.created"] = true

	h.PublishForUser(1, "notification.created", map[string]string{"id": "1"})

	assert.Empty(t, conn.send)
}

func projectConn(userID int64) *Connection {
	return &Connection{
		userID:        userID,
		authenticated: true,
		subscriptions: make(map[string]bool),
		projects:      make(map[int64]bool),
		send:          make(chan OutgoingMessage, 16),
	}
}

func TestHubProjectSubscriptions(t *testing.T) {
	t.Run("publishes to connections subscribed to the project", func(t *testing.T) {
		h := NewHub()
		subscribed := projectConn(1)
		other := projectConn(2)
		h.Register(subscribed)
		h.Register(other)
		h.SubscribeProject(subscribed, 7)

		h.PublishForProject(7, "task.updated", &ProjectTaskEvent{ProjectID: 7, TaskID: 3}, nil)

		msg := <-subscribed.send
		assert.Equal(t, "task.updated", msg.Event)
		assert.Equal(t, int64(3), msg.Data.(*ProjectTaskEvent).TaskID)
		assert.Empty(t, other.send)
		assert.True(t, subscribed.IsSubscribedToProject(7))
	})

	t.Run("skips a connection after it unsubscribed", func(t *testing.T) {
		h := NewHub()
		conn := projectConn(1)
		h.Register(conn)
		h.SubscribeProject(conn, 7)
		h.UnsubscribeProject(conn, 7)

		h.PublishForProject(7, "task.updated", nil, nil)

		assert.Empty(t, conn.send)
		assert.False(t, conn.IsSubscribedToProject(7))
		_, indexed := h.projects[7]
		assert.False(t, indexed, "an empty project entry must be removed")
	})

	t.Run("delivers a move once to a connection subscribed to both projects", func(t *testing.T) {
		h := NewHub()
		both := projectConn(1)
		oldOnly := projectConn(2)
		h.Register(both)
		h.Register(oldOnly)
		h.SubscribeProject(both, 7)
		h.SubscribeProject(both, 8)
		h.SubscribeProject(oldOnly, 8)

		h.PublishForProjects([]int64{7, 8}, "task.updated", nil, nil)

		assert.Len(t, both.send, 1)
		assert.Len(t, oldOnly.send, 1)
	})

	t.Run("drops the subscription of a user the check rejects and tells the connection", func(t *testing.T) {
		h := NewHub()
		revoked := projectConn(1)
		allowed := projectConn(2)
		h.Register(revoked)
		h.Register(allowed)
		h.SubscribeProject(revoked, 7)
		h.SubscribeProject(allowed, 7)

		h.PublishForProject(7, "task.updated", nil, func(userID, _ int64) bool {
			return userID != 1
		})

		require.Len(t, revoked.send, 1)
		msg := <-revoked.send
		assert.Equal(t, ActionUnsubscribed, msg.Action)
		assert.Equal(t, EventProject, msg.Event)
		assert.Equal(t, int64(7), msg.ProjectID)
		assert.Equal(t, ReasonForbidden, msg.Reason)
		assert.False(t, revoked.IsSubscribedToProject(7))
		require.Len(t, allowed.send, 1)
		assert.Equal(t, "task.updated", (<-allowed.send).Event)
	})

	t.Run("asks the check once per user and project", func(t *testing.T) {
		h := NewHub()
		first := projectConn(1)
		second := projectConn(1)
		h.Register(first)
		h.Register(second)
		h.SubscribeProject(first, 7)
		h.SubscribeProject(second, 7)

		calls := 0
		h.PublishForProject(7, "task.updated", nil, func(_, _ int64) bool {
			calls++
			return true
		})

		assert.Equal(t, 1, calls)
		assert.Len(t, first.send, 1)
		assert.Len(t, second.send, 1)
	})

	t.Run("unregister clears the project index", func(t *testing.T) {
		h := NewHub()
		conn := projectConn(1)
		h.Register(conn)
		h.SubscribeProject(conn, 7)

		h.Unregister(conn)

		assert.Empty(t, h.projects)
		h.PublishForProject(7, "task.updated", nil, nil)
		assert.Empty(t, conn.send)
	})
}
