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

	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// subscribedProjectConn registers a connection of user 1 subscribed to projectID.
func subscribedProjectConn(projectID int64) *Connection {
	conn := projectConn(1)
	GetHub().Register(conn)
	GetHub().SubscribeProject(conn, projectID)
	return conn
}

func TestProjectTaskListener(t *testing.T) {
	t.Run("pushes ids and placements to the project's subscribers", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		projectAccess.reset()
		conn := subscribedProjectConn(1)
		other := subscribedProjectConn(2)

		events.TestListener(t,
			&models.TaskUpdatedEvent{Task: &models.Task{ID: 1, ProjectID: 1}, Doer: &user.User{ID: 1}},
			&ProjectTaskListener{wsEvent: "task.updated"},
		)

		require.Len(t, conn.send, 1)
		msg := <-conn.send
		assert.Equal(t, "task.updated", msg.Event)
		payload, ok := msg.Data.(*ProjectTaskEvent)
		require.True(t, ok)
		assert.Equal(t, int64(1), payload.ProjectID)
		assert.Equal(t, int64(1), payload.TaskID)
		assert.Equal(t, int64(1), payload.DoerID)
		assert.Zero(t, payload.OldProjectID)
		assert.Equal(t, []*models.TaskPlacement{
			{ProjectViewID: 1, Position: 2},
			{ProjectViewID: 4, BucketID: 1},
		}, payload.Placements)
		assert.Empty(t, other.send, "a subscriber of another project must not receive it")
	})

	t.Run("a delete carries no placements", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		projectAccess.reset()
		conn := subscribedProjectConn(1)

		events.TestListener(t,
			&models.TaskDeletedEvent{Task: &models.Task{ID: 1, ProjectID: 1}, Doer: &user.User{ID: 1}},
			&ProjectTaskListener{wsEvent: "task.deleted"},
		)

		require.Len(t, conn.send, 1)
		msg := <-conn.send
		assert.Equal(t, "task.deleted", msg.Event)
		assert.Empty(t, msg.Data.(*ProjectTaskEvent).Placements)
	})

	t.Run("a move also reaches the previous project's subscribers", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		projectAccess.reset()
		oldOnly := subscribedProjectConn(1)
		newOnly := subscribedProjectConn(2)

		events.TestListener(t,
			&models.TaskUpdatedEvent{Task: &models.Task{ID: 1, ProjectID: 2}, Doer: &user.User{ID: 1}, OldProjectID: 1},
			&ProjectTaskListener{wsEvent: "task.updated"},
		)

		require.Len(t, oldOnly.send, 1)
		require.Len(t, newOnly.send, 1)
		payload := (<-oldOnly.send).Data.(*ProjectTaskEvent)
		assert.Equal(t, int64(2), payload.ProjectID)
		assert.Equal(t, int64(1), payload.OldProjectID)
	})

	t.Run("drops a subscriber who lost access to the project", func(t *testing.T) {
		s := setupDBTest(t)
		InitHub()
		projectAccess.reset()
		// Project 3 is shared with user 1 through users_projects only.
		conn := subscribedProjectConn(3)
		unshareProject(t, s, 3, 1)
		require.NoError(t, s.Commit())

		events.TestListener(t,
			&models.TaskUpdatedEvent{Task: &models.Task{ID: 32, ProjectID: 3}, Doer: &user.User{ID: 3}},
			&ProjectTaskListener{wsEvent: "task.updated"},
		)

		require.Len(t, conn.send, 1)
		msg := <-conn.send
		assert.Equal(t, ActionUnsubscribed, msg.Action)
		assert.Equal(t, int64(3), msg.ProjectID)
		assert.Equal(t, ReasonForbidden, msg.Reason)
		assert.False(t, conn.IsSubscribedToProject(3))
	})

	t.Run("keeps delivering within the access cache window after access was lost", func(t *testing.T) {
		s := setupDBTest(t)
		InitHub()
		projectAccess.reset()
		conn := subscribedProjectConn(3)
		projectAccess.set(1, 3, true)
		unshareProject(t, s, 3, 1)
		require.NoError(t, s.Commit())

		events.TestListener(t,
			&models.TaskUpdatedEvent{Task: &models.Task{ID: 32, ProjectID: 3}, Doer: &user.User{ID: 3}},
			&ProjectTaskListener{wsEvent: "task.updated"},
		)

		require.Len(t, conn.send, 1)
		assert.Equal(t, "task.updated", (<-conn.send).Event)
	})

	t.Run("ignores an event without a task", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := subscribedProjectConn(1)

		events.TestListener(t, &models.TaskUpdatedEvent{}, &ProjectTaskListener{wsEvent: "task.updated"})

		assert.Empty(t, conn.send)
	})
}

func TestCanReadProject(t *testing.T) {
	setupDBTest(t)

	can, err := canReadProject(1, 1)
	require.NoError(t, err)
	assert.True(t, can, "the owner can read")

	can, err = canReadProject(1, 20)
	require.NoError(t, err)
	assert.False(t, can, "a project not shared with the user cannot be read")

	can, err = canReadProject(1, 99999)
	require.NoError(t, err)
	assert.False(t, can, "a missing project reads as no access")
}

func TestProjectCommentListener(t *testing.T) {
	t.Run("pushes the comment ids to the task project's subscribers", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		projectAccess.reset()
		conn := subscribedProjectConn(1)
		other := subscribedProjectConn(2)

		events.TestListener(t,
			&models.TaskCommentCreatedEvent{
				Task:    &models.Task{ID: 1, ProjectID: 1},
				Comment: &models.TaskComment{ID: 5, Comment: "secret text"},
				Doer:    &user.User{ID: 1},
			},
			&ProjectCommentListener{wsEvent: "task.comment.created"},
		)

		require.Len(t, conn.send, 1)
		msg := <-conn.send
		assert.Equal(t, "task.comment.created", msg.Event)
		assert.Equal(t, &ProjectCommentEvent{ProjectID: 1, TaskID: 1, CommentID: 5, DoerID: 1}, msg.Data)
		assert.Empty(t, other.send)
	})

	t.Run("ignores an event without a comment", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		projectAccess.reset()
		conn := subscribedProjectConn(1)

		events.TestListener(t,
			&models.TaskCommentDeletedEvent{Task: &models.Task{ID: 1, ProjectID: 1}},
			&ProjectCommentListener{wsEvent: "task.comment.deleted"},
		)

		assert.Empty(t, conn.send)
	})
}

func TestProjectBucketListener(t *testing.T) {
	t.Run("an update carries the bucket", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		projectAccess.reset()
		conn := subscribedProjectConn(1)
		other := subscribedProjectConn(2)

		events.TestListener(t,
			&models.BucketUpdatedEvent{
				Bucket:    &models.Bucket{ID: 1, ProjectViewID: 4, Title: "Doing", Limit: 3, Position: 2},
				ProjectID: 1,
				Doer:      &user.User{ID: 1},
			},
			&ProjectBucketListener{wsEvent: "bucket.updated"},
		)

		require.Len(t, conn.send, 1)
		msg := <-conn.send
		assert.Equal(t, "bucket.updated", msg.Event)
		assert.Equal(t, &ProjectBucketEvent{
			ProjectID:     1,
			ProjectViewID: 4,
			BucketID:      1,
			DoerID:        1,
			Bucket:        &ProjectBucket{ID: 1, ProjectViewID: 4, Title: "Doing", Limit: 3, Position: 2},
		}, msg.Data)
		assert.Empty(t, other.send)
	})

	t.Run("a delete names where the tasks went", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		projectAccess.reset()
		conn := subscribedProjectConn(1)

		events.TestListener(t,
			&models.BucketDeletedEvent{
				Bucket:          &models.Bucket{ID: 2, ProjectViewID: 4, Title: "gone"},
				ProjectID:       1,
				MovedToBucketID: 1,
			},
			&ProjectBucketListener{wsEvent: "bucket.deleted"},
		)

		require.Len(t, conn.send, 1)
		assert.Equal(t, &ProjectBucketEvent{
			ProjectID:       1,
			ProjectViewID:   4,
			BucketID:        2,
			MovedToBucketID: 1,
		}, (<-conn.send).Data)
	})

	t.Run("drops a subscriber who lost access", func(t *testing.T) {
		s := setupDBTest(t)
		InitHub()
		projectAccess.reset()
		conn := subscribedProjectConn(3)
		unshareProject(t, s, 3, 1)
		require.NoError(t, s.Commit())

		events.TestListener(t,
			&models.BucketCreatedEvent{Bucket: &models.Bucket{ID: 40, ProjectViewID: 12, Title: "x"}, ProjectID: 3},
			&ProjectBucketListener{wsEvent: "bucket.created"},
		)

		require.Len(t, conn.send, 1)
		assert.Equal(t, ActionUnsubscribed, (<-conn.send).Action)
	})

	t.Run("a saved filter view reaches nobody", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		projectAccess.reset()
		conn := subscribedProjectConn(1)

		events.TestListener(t,
			&models.BucketCreatedEvent{Bucket: &models.Bucket{ID: 40, ProjectViewID: 60}, ProjectID: -2},
			&ProjectBucketListener{wsEvent: "bucket.created"},
		)

		assert.Empty(t, conn.send)
	})
}

func TestPositionsRecalculatedListener(t *testing.T) {
	t.Run("names the recalculated view", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		projectAccess.reset()
		conn := subscribedProjectConn(1)
		other := subscribedProjectConn(2)

		events.TestListener(t,
			&models.TaskPositionsRecalculatedEvent{
				NewTaskPositions: []*models.TaskPosition{{TaskID: 1, ProjectViewID: 4, Position: 1}},
				ProjectID:        1,
				ProjectViewID:    4,
			},
			&PositionsRecalculatedListener{},
		)

		require.Len(t, conn.send, 1)
		msg := <-conn.send
		assert.Equal(t, "task.positions.recalculated", msg.Event)
		assert.Equal(t, &ProjectViewEvent{ProjectID: 1, ProjectViewID: 4}, msg.Data)
		assert.Empty(t, other.send)
	})

	t.Run("a saved filter view reaches nobody", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		projectAccess.reset()
		conn := subscribedProjectConn(1)

		events.TestListener(t,
			&models.TaskPositionsRecalculatedEvent{ProjectID: -2, ProjectViewID: 60},
			&PositionsRecalculatedListener{},
		)

		assert.Empty(t, conn.send)
	})
}
