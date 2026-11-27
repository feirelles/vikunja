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
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskCollection_HideSubtasks(t *testing.T) {
	taskIDs := func(t *testing.T, u *user.User, tc *TaskCollection) []int64 {
		t.Helper()
		s := db.NewSession()
		defer s.Close()
		result, _, _, err := tc.ReadAll(s, u, "", 1, 500)
		require.NoError(t, err)
		ids := []int64{}
		for _, task := range result.([]*Task) {
			ids = append(ids, task.ID)
		}
		return ids
	}

	t.Run("same project", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		u := &user.User{ID: 1}

		// Task 29 is a subtask of task 1, both in project 1.
		assert.Contains(t, taskIDs(t, u, &TaskCollection{ProjectID: 1}), int64(29))
		hidden := taskIDs(t, u, &TaskCollection{ProjectID: 1, HideSubtasks: true})
		assert.NotContains(t, hidden, int64(29))
		assert.Contains(t, hidden, int64(1))
	})
	t.Run("parent in another project", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		u := &user.User{ID: 15}

		// Task 44 (project 38) is a subtask of task 41 (project 36); user 15 owns both.
		assert.Contains(t, taskIDs(t, u, &TaskCollection{ProjectID: 38}), int64(44))
		assert.NotContains(t, taskIDs(t, u, &TaskCollection{ProjectID: 38, HideSubtasks: true}), int64(44))
	})
	t.Run("parent the user cannot read", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		u := &user.User{ID: 1}
		s := db.NewSession()
		defer s.Close()
		// Task 14 is in project 5, which user 1 cannot see.
		_, err := s.Insert(&TaskRelation{TaskID: 2, OtherTaskID: 14, RelationKind: RelationKindParenttask, CreatedByID: 1})
		require.NoError(t, err)
		require.NoError(t, s.Commit())

		assert.Contains(t, taskIDs(t, u, &TaskCollection{ProjectID: 1, HideSubtasks: true}), int64(2))
	})
	t.Run("kanban buckets", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		u := &user.User{ID: 1}
		s := db.NewSession()
		defer s.Close()

		bucketTaskIDs := func(hide bool) []int64 {
			tc := &TaskCollection{ProjectID: 1, ProjectViewID: 4, HideSubtasks: hide}
			result, _, _, err := tc.ReadAll(s, u, "", 1, 500)
			require.NoError(t, err)
			ids := []int64{}
			for _, b := range result.([]*Bucket) {
				for _, task := range b.Tasks {
					ids = append(ids, task.ID)
				}
			}
			return ids
		}
		assert.Contains(t, bucketTaskIDs(false), int64(29))
		assert.NotContains(t, bucketTaskIDs(true), int64(29))
	})
}
