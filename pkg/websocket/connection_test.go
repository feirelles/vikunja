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
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectionSubscribeUnsubscribe(t *testing.T) {
	conn := &Connection{
		userID:        1,
		authenticated: true,
		subscriptions: make(map[string]bool),
		send:          make(chan OutgoingMessage, 16),
	}

	conn.Subscribe("notification.created")
	assert.True(t, conn.IsSubscribed("notification.created"))

	conn.Unsubscribe("notification.created")
	assert.False(t, conn.IsSubscribed("notification.created"))
}

func TestConnectionIsSubscribedReturnsFalseForUnknownEvent(t *testing.T) {
	conn := &Connection{
		userID:        1,
		authenticated: true,
		subscriptions: make(map[string]bool),
		send:          make(chan OutgoingMessage, 16),
	}

	assert.False(t, conn.IsSubscribed("something"))
}

func TestConnectionAcceptsValidEvent(t *testing.T) {
	hub := NewHub()
	conn := &Connection{
		hub:           hub,
		userID:        1,
		authenticated: true,
		subscriptions: make(map[string]bool),
		send:          make(chan OutgoingMessage, 16),
	}
	hub.Register(conn)

	conn.handleMessage(context.Background(), IncomingMessage{Action: ActionSubscribe, Event: "notification.created"})

	assert.True(t, conn.IsSubscribed("notification.created"))
}

func TestConnectionRejectsInvalidEvent(t *testing.T) {
	conn := &Connection{
		userID:        1,
		authenticated: true,
		subscriptions: make(map[string]bool),
		send:          make(chan OutgoingMessage, 16),
	}

	conn.handleMessage(context.Background(), IncomingMessage{Action: ActionSubscribe, Event: "notifications"})

	msg := <-conn.send
	assert.Equal(t, "invalid_event", msg.Error)
	assert.False(t, conn.IsSubscribed("notifications"))
}

func TestConnectionAllowsTimerEvents(t *testing.T) {
	conn := &Connection{
		userID:        1,
		authenticated: true,
		subscriptions: make(map[string]bool),
		send:          make(chan OutgoingMessage, 16),
	}

	for _, event := range []string{"timer.created", "timer.updated"} {
		conn.handleMessage(context.Background(), IncomingMessage{Action: ActionSubscribe, Event: event})
		assert.True(t, conn.IsSubscribed(event), "client must be able to subscribe to %s", event)
	}
}

func TestConnectionRejectsActionsBeforeAuth(t *testing.T) {
	conn := &Connection{
		userID:        0, // not authenticated
		authenticated: false,
		subscriptions: make(map[string]bool),
		send:          make(chan OutgoingMessage, 16),
	}

	// Try to subscribe before auth - should be rejected
	conn.handleMessage(context.Background(), IncomingMessage{Action: ActionSubscribe, Event: "notification.created"})

	// Should have sent an error
	msg := <-conn.send
	assert.Equal(t, "auth_required", msg.Error)
	assert.False(t, conn.IsSubscribed("notification.created"))
}

func stubProjectAccess(t *testing.T, can bool, err error) *[]int64 {
	t.Helper()
	original := canReadProject
	checked := &[]int64{}
	canReadProject = func(_, projectID int64) (bool, error) {
		*checked = append(*checked, projectID)
		return can, err
	}
	projectAccess.reset()
	t.Cleanup(func() {
		canReadProject = original
		projectAccess.reset()
	})
	return checked
}

func TestConnectionProjectSubscribe(t *testing.T) {
	t.Run("subscribes after the read check passes", func(t *testing.T) {
		checked := stubProjectAccess(t, true, nil)
		hub := NewHub()
		conn := projectConn(1)
		conn.hub = hub
		hub.Register(conn)

		conn.handleMessage(context.Background(), IncomingMessage{Action: ActionSubscribe, Event: EventProject, ProjectID: 7})

		assert.Equal(t, []int64{7}, *checked)
		assert.True(t, conn.IsSubscribedToProject(7))
		assert.Empty(t, conn.send)
		allowed, err := projectAccess.allowed(1, 7, func() (bool, error) { return false, nil })
		require.NoError(t, err)
		assert.True(t, allowed, "the subscribe verdict must seed the access cache")
	})

	t.Run("answers forbidden without subscribing", func(t *testing.T) {
		stubProjectAccess(t, false, nil)
		hub := NewHub()
		conn := projectConn(1)
		conn.hub = hub
		hub.Register(conn)

		conn.handleMessage(context.Background(), IncomingMessage{Action: ActionSubscribe, Event: EventProject, ProjectID: 7})

		msg := <-conn.send
		assert.Equal(t, ReasonForbidden, msg.Error)
		assert.Equal(t, EventProject, msg.Event)
		assert.Equal(t, int64(7), msg.ProjectID)
		assert.False(t, conn.IsSubscribedToProject(7))
	})

	t.Run("rejects a missing project id", func(t *testing.T) {
		checked := stubProjectAccess(t, true, nil)
		hub := NewHub()
		conn := projectConn(1)
		conn.hub = hub

		conn.handleMessage(context.Background(), IncomingMessage{Action: ActionSubscribe, Event: EventProject})

		msg := <-conn.send
		assert.Equal(t, "invalid_project", msg.Error)
		assert.Empty(t, *checked)
	})

	t.Run("reports a failed check instead of subscribing", func(t *testing.T) {
		stubProjectAccess(t, false, errors.New("db down"))
		hub := NewHub()
		conn := projectConn(1)
		conn.hub = hub

		conn.handleMessage(context.Background(), IncomingMessage{Action: ActionSubscribe, Event: EventProject, ProjectID: 7})

		msg := <-conn.send
		assert.Equal(t, "internal_error", msg.Error)
		assert.False(t, conn.IsSubscribedToProject(7))
	})

	t.Run("requires auth", func(t *testing.T) {
		checked := stubProjectAccess(t, true, nil)
		conn := projectConn(0)
		conn.authenticated = false

		conn.handleMessage(context.Background(), IncomingMessage{Action: ActionSubscribe, Event: EventProject, ProjectID: 7})

		msg := <-conn.send
		assert.Equal(t, "auth_required", msg.Error)
		assert.Empty(t, *checked)
	})

	t.Run("unsubscribes from the project", func(t *testing.T) {
		stubProjectAccess(t, true, nil)
		hub := NewHub()
		conn := projectConn(1)
		conn.hub = hub
		hub.Register(conn)
		conn.handleMessage(context.Background(), IncomingMessage{Action: ActionSubscribe, Event: EventProject, ProjectID: 7})

		conn.handleMessage(context.Background(), IncomingMessage{Action: ActionUnsubscribe, Event: EventProject, ProjectID: 7})

		assert.False(t, conn.IsSubscribedToProject(7))
		assert.Empty(t, hub.projects)
	})
}
