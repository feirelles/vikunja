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
	"encoding/json"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/models"

	"github.com/ThreeDotsLabs/watermill/message"
)

// ProjectTaskListener pushes a thin task event (ids and placements, see
// ProjectTaskEvent) to the connections subscribed to the task's project.
// wsEvent is the event name pushed: task.created, task.updated or
// task.deleted. A move also reaches the subscribers of the previous
// project, so they can drop the task.
type ProjectTaskListener struct {
	wsEvent string
}

func (l *ProjectTaskListener) Name() string { return "websocket.project.push." + l.wsEvent }

func (l *ProjectTaskListener) Handle(msg *message.Message) error {
	// All task events share the {task, doer} shape; only the ids are needed.
	var event struct {
		Task *struct {
			ID        int64 `json:"id"`
			ProjectID int64 `json:"project_id"`
		} `json:"task"`
		Doer *struct {
			ID int64 `json:"id"`
		} `json:"doer"`
		OldProjectID int64 `json:"old_project_id"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return err
	}
	if event.Task == nil || event.Task.ID == 0 || event.Task.ProjectID == 0 {
		return nil
	}

	hub := GetHub()
	if hub == nil {
		log.Warningf("WebSocket: hub not initialized, skipping %s push", l.wsEvent)
		return nil
	}

	payload := &ProjectTaskEvent{
		ProjectID: event.Task.ProjectID,
		TaskID:    event.Task.ID,
	}
	if event.Doer != nil {
		payload.DoerID = event.Doer.ID
	}
	if event.OldProjectID != 0 && event.OldProjectID != event.Task.ProjectID {
		payload.OldProjectID = event.OldProjectID
	}
	if l.wsEvent != "task.deleted" {
		payload.Placements = loadPlacements(event.Task.ID, event.Task.ProjectID)
	}

	projectIDs := []int64{payload.ProjectID}
	if payload.OldProjectID != 0 {
		projectIDs = append(projectIDs, payload.OldProjectID)
	}
	hub.PublishForProjects(projectIDs, l.wsEvent, payload, userMayReadProject)
	return nil
}

// loadPlacements reads the task's bucket and position per view. A failure is
// logged and leaves the placements empty: the client then keeps the card where
// it is and still refreshes the task.
func loadPlacements(taskID, projectID int64) []*models.TaskPlacement {
	s := db.NewSession()
	defer s.Close()
	placements, err := models.GetTaskPlacements(s, taskID, projectID)
	if err != nil {
		log.Errorf("WebSocket: failed to load placements of task %d: %v", taskID, err)
		return nil
	}
	return placements
}

// userMayReadProject re-validates a subscription on delivery through the access cache.
func userMayReadProject(userID, projectID int64) bool {
	allowed, err := projectAccess.allowed(userID, projectID, func() (bool, error) {
		return canReadProject(userID, projectID)
	})
	if err != nil {
		log.Errorf("WebSocket: could not re-check access of user %d to project %d: %v", userID, projectID, err)
		return false
	}
	return allowed
}

// RegisterProjectListeners registers the task listeners that feed project subscriptions.
func RegisterProjectListeners() {
	events.RegisterListener((&models.TaskCreatedEvent{}).Name(), &ProjectTaskListener{wsEvent: "task.created"})
	events.RegisterListener((&models.TaskUpdatedEvent{}).Name(), &ProjectTaskListener{wsEvent: "task.updated"})
	events.RegisterListener((&models.TaskDeletedEvent{}).Name(), &ProjectTaskListener{wsEvent: "task.deleted"})
}
