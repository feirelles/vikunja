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

package models

import (
	"context"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTaskPlacements(t *testing.T) {
	t.Run("merges bucket and position rows per view in view order", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		placements, err := GetTaskPlacements(s, 1, 1)
		require.NoError(t, err)

		// Task 1 has a position in the list view (1) and a bucket in the kanban view (4).
		assert.Equal(t, []*TaskPlacement{
			{ProjectViewID: 1, Position: 2},
			{ProjectViewID: 4, BucketID: 1},
		}, placements)
	})

	t.Run("ignores views of other projects", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		placements, err := GetTaskPlacements(s, 1, 2)
		require.NoError(t, err)
		assert.Empty(t, placements)
	})
}

func TestTaskUpdatedEvent_OldProjectID(t *testing.T) {
	u := &user.User{ID: 1}

	t.Run("is set when the task moves to another project", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		events.ClearDispatchedEvents()

		task := &Task{ID: 1, ProjectID: 2}
		require.NoError(t, task.Update(s, u))
		require.NoError(t, s.Commit())
		events.DispatchPending(context.Background(), s)

		dispatched := events.GetDispatchedEvents((&TaskUpdatedEvent{}).Name())
		require.Len(t, dispatched, 1)
		event := dispatched[0].(*TaskUpdatedEvent)
		assert.Equal(t, int64(1), event.OldProjectID)
		assert.Equal(t, int64(2), event.Task.ProjectID)
	})

	t.Run("is zero when the task stays in its project", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		events.ClearDispatchedEvents()

		task := &Task{ID: 1, Title: "renamed"}
		require.NoError(t, task.Update(s, u))
		require.NoError(t, s.Commit())
		events.DispatchPending(context.Background(), s)

		dispatched := events.GetDispatchedEvents((&TaskUpdatedEvent{}).Name())
		require.Len(t, dispatched, 1)
		assert.Zero(t, dispatched[0].(*TaskUpdatedEvent).OldProjectID)
	})
}
