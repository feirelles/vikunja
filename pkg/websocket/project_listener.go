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
// wsEvent is the event name pushed: task.created, task.updated, task.deleted
// or task.restored. A move also reaches the subscribers of the previous
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

// ProjectCommentListener pushes a thin comment event (see ProjectCommentEvent)
// to the subscribers of the comment's task project. wsEvent is the model
// event name: task.comment.created, task.comment.edited or task.comment.deleted.
type ProjectCommentListener struct {
	wsEvent string
}

func (l *ProjectCommentListener) Name() string { return "websocket.project.push." + l.wsEvent }

func (l *ProjectCommentListener) Handle(msg *message.Message) error {
	var event struct {
		Task *struct {
			ID        int64 `json:"id"`
			ProjectID int64 `json:"project_id"`
		} `json:"task"`
		Comment *struct {
			ID int64 `json:"id"`
		} `json:"comment"`
		Doer *struct {
			ID int64 `json:"id"`
		} `json:"doer"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return err
	}
	if event.Task == nil || event.Task.ID == 0 || event.Task.ProjectID == 0 || event.Comment == nil || event.Comment.ID == 0 {
		return nil
	}

	hub := GetHub()
	if hub == nil {
		log.Warningf("WebSocket: hub not initialized, skipping %s push", l.wsEvent)
		return nil
	}

	payload := &ProjectCommentEvent{
		ProjectID: event.Task.ProjectID,
		TaskID:    event.Task.ID,
		CommentID: event.Comment.ID,
	}
	if event.Doer != nil {
		payload.DoerID = event.Doer.ID
	}
	hub.PublishForProject(payload.ProjectID, l.wsEvent, payload, userMayReadProject)
	return nil
}

// ProjectBucketListener pushes bucket.created, bucket.updated and
// bucket.deleted (see ProjectBucketEvent) to the subscribers of the bucket's project.
type ProjectBucketListener struct {
	wsEvent string
}

func (l *ProjectBucketListener) Name() string { return "websocket.project.push." + l.wsEvent }

func (l *ProjectBucketListener) Handle(msg *message.Message) error {
	var event struct {
		Bucket *struct {
			ID            int64   `json:"id"`
			ProjectViewID int64   `json:"project_view_id"`
			Title         string  `json:"title"`
			Limit         int64   `json:"limit"`
			Position      float64 `json:"position"`
		} `json:"bucket"`
		ProjectID       int64 `json:"project_id"`
		MovedToBucketID int64 `json:"moved_to_bucket_id"`
		Doer            *struct {
			ID int64 `json:"id"`
		} `json:"doer"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return err
	}
	// Saved filter views belong to a negative pseudo project nobody subscribes to.
	if event.Bucket == nil || event.Bucket.ID == 0 || event.ProjectID <= 0 {
		return nil
	}

	hub := GetHub()
	if hub == nil {
		log.Warningf("WebSocket: hub not initialized, skipping %s push", l.wsEvent)
		return nil
	}

	payload := &ProjectBucketEvent{
		ProjectID:     event.ProjectID,
		ProjectViewID: event.Bucket.ProjectViewID,
		BucketID:      event.Bucket.ID,
	}
	if event.Doer != nil {
		payload.DoerID = event.Doer.ID
	}
	if l.wsEvent == "bucket.deleted" {
		payload.MovedToBucketID = event.MovedToBucketID
	} else {
		payload.Bucket = &ProjectBucket{
			ID:            event.Bucket.ID,
			ProjectViewID: event.Bucket.ProjectViewID,
			Title:         event.Bucket.Title,
			Limit:         event.Bucket.Limit,
			Position:      event.Bucket.Position,
		}
	}
	hub.PublishForProject(payload.ProjectID, l.wsEvent, payload, userMayReadProject)
	return nil
}

// PositionsRecalculatedListener tells a view's project subscribers that all of
// the view's stored positions changed (task.positions.recalculated).
type PositionsRecalculatedListener struct{}

func (l *PositionsRecalculatedListener) Name() string {
	return "websocket.project.push.task.positions.recalculated"
}

func (l *PositionsRecalculatedListener) Handle(msg *message.Message) error {
	var event struct {
		ProjectID     int64 `json:"project_id"`
		ProjectViewID int64 `json:"project_view_id"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return err
	}
	if event.ProjectID <= 0 || event.ProjectViewID == 0 {
		return nil
	}

	hub := GetHub()
	if hub == nil {
		log.Warningf("WebSocket: hub not initialized, skipping task.positions.recalculated push")
		return nil
	}

	hub.PublishForProject(event.ProjectID, "task.positions.recalculated", &ProjectViewEvent{
		ProjectID:     event.ProjectID,
		ProjectViewID: event.ProjectViewID,
	}, userMayReadProject)
	return nil
}

// RegisterProjectListeners registers the task, comment, bucket and position listeners that feed project subscriptions.
func RegisterProjectListeners() {
	events.RegisterListener((&models.TaskCreatedEvent{}).Name(), &ProjectTaskListener{wsEvent: "task.created"})
	events.RegisterListener((&models.TaskUpdatedEvent{}).Name(), &ProjectTaskListener{wsEvent: "task.updated"})
	events.RegisterListener((&models.TaskDeletedEvent{}).Name(), &ProjectTaskListener{wsEvent: "task.deleted"})
	events.RegisterListener((&models.TaskRestoredEvent{}).Name(), &ProjectTaskListener{wsEvent: "task.restored"})

	for _, e := range []events.Event{&models.TaskCommentCreatedEvent{}, &models.TaskCommentUpdatedEvent{}, &models.TaskCommentDeletedEvent{}} {
		events.RegisterListener(e.Name(), &ProjectCommentListener{wsEvent: e.Name()})
	}
	for _, e := range []events.Event{&models.BucketCreatedEvent{}, &models.BucketUpdatedEvent{}, &models.BucketDeletedEvent{}} {
		events.RegisterListener(e.Name(), &ProjectBucketListener{wsEvent: e.Name()})
	}
	events.RegisterListener((&models.TaskPositionsRecalculatedEvent{}).Name(), &PositionsRecalculatedListener{})
}
