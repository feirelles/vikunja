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

import "code.vikunja.io/api/pkg/models"

const (
	// Client actions
	ActionAuth        = "auth"
	ActionSubscribe   = "subscribe"
	ActionUnsubscribe = "unsubscribe"

	// Server actions
	ActionAuthSuccess  = "auth.success"
	ActionUnsubscribed = "unsubscribed"

	// EventProject is the subscribe/unsubscribe scope for a project's task
	// events. The frame carries the project id next to it; the pushed
	// messages are named task.created, task.updated, task.deleted and
	// task.restored.
	EventProject = "project"

	// ReasonForbidden is the reason sent with an unsubscribed action when the
	// user lost read access to the project.
	ReasonForbidden = "forbidden"
)

// IncomingMessage represents a message from the client.
type IncomingMessage struct {
	Action string `json:"action"`
	// Token is set for auth action.
	Token string `json:"token,omitempty"`
	// Event is set for subscribe/unsubscribe actions.
	Event string `json:"event,omitempty"`
	// ProjectID is set for subscribe/unsubscribe actions with the project event.
	ProjectID int64 `json:"project_id,omitempty"`
}

// OutgoingMessage represents a message from the server to the client.
// Exactly one of Event, Error, or Action will be set.
type OutgoingMessage struct {
	// Event identifies the event type. On push messages (e.g. "notification.created"),
	// it carries the event name. On error responses for subscribe/unsubscribe,
	// it identifies which event caused the error.
	Event string `json:"event,omitempty"`
	// Error is set for error responses (e.g. "forbidden").
	Error string `json:"error,omitempty"`
	// Action is set for server-initiated actions (e.g. "auth.success", "unsubscribed").
	Action string `json:"action,omitempty"`
	// Success is set for auth.success action.
	Success bool `json:"success,omitempty"`
	// Reason provides context for server-initiated actions.
	Reason string `json:"reason,omitempty"`
	// ProjectID identifies the project on responses to a project subscription.
	ProjectID int64 `json:"project_id,omitempty"`
	// Data carries the event payload.
	Data any `json:"data,omitempty"`
}

// ProjectTaskEvent is the payload of the task.* events pushed to project
// subscribers. It carries ids only: clients load the task through the REST
// API, which applies the reader's permissions and per-user fields.
type ProjectTaskEvent struct {
	ProjectID int64 `json:"project_id"`
	TaskID    int64 `json:"task_id"`
	// DoerID is the user who made the change, zero when unknown.
	DoerID int64 `json:"doer_id,omitempty"`
	// OldProjectID is set on task.updated when the task was moved out of another project.
	OldProjectID int64 `json:"old_project_id,omitempty"`
	// Placements is the task's bucket and position in each view of its project
	// that stores one. Empty on task.deleted.
	Placements []*models.TaskPlacement `json:"placements,omitempty"`
}
