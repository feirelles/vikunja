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

package migration

import (
	"testing"
	"time"

	"code.vikunja.io/api/pkg/db"

	"github.com/stretchr/testify/require"
)

func TestMigrationTaskQuestions20261127104016(t *testing.T) {
	engine, err := db.CreateTestEngine()
	require.NoError(t, err)
	table := taskQuestions20261127104016{}
	require.NoError(t, engine.DropTables(table))
	require.NoError(t, createTaskQuestions20261127104016(engine))
	t.Cleanup(func() {
		require.NoError(t, engine.DropTables(table))
	})

	answeredAt := time.Now().Truncate(time.Second)
	_, err = engine.Insert(&taskQuestions20261127104016{
		TaskID:         1,
		AuthorID:       1,
		Title:          "Which?",
		Options:        []map[string]any{{"key": "A", "label": "One"}},
		Recommendation: map[string]string{"key": "A"},
		Status:         "answered",
		AnswerOption:   "A",
		AnsweredByID:   1,
		AnsweredAt:     &answeredAt,
	})
	require.NoError(t, err)
	_, err = engine.Insert(&taskQuestions20261127104016{TaskID: 1, AuthorID: 1, Title: "Open"})
	require.NoError(t, err)

	got := []taskQuestions20261127104016{}
	require.NoError(t, engine.OrderBy("id").Find(&got))
	require.Len(t, got, 2)
	require.Equal(t, "A", got[0].Options[0]["key"])
	require.Equal(t, "A", got[0].Recommendation["key"])
	require.True(t, got[0].AnsweredAt.Equal(answeredAt))
	require.Nil(t, got[1].AnsweredAt)

	tables, err := engine.DBMetas()
	require.NoError(t, err)
	for _, tbl := range tables {
		if tbl.Name != "task_questions" {
			continue
		}
		for _, col := range []string{"task_id", "status", "answer_option", "answered_at", "options", "recommendation"} {
			require.NotNilf(t, tbl.GetColumn(col), "missing column %s", col)
		}
		return
	}
	t.Fatal("task_questions was not created")
}
