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
	"encoding/json"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskQuestion_Create(t *testing.T) {
	u := &user.User{ID: 1}

	t.Run("normal", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		events.Fake()
		s := db.NewSession()
		defer s.Close()

		q := &TaskQuestion{
			TaskID: 1,
			Title:  "Ship it?",
			Options: []*TaskQuestionOption{
				{Key: "A", Label: "Yes"},
				{Key: "B", Label: "No"},
			},
			Recommendation: &TaskQuestionRecommendation{Key: "A", Reason: "tests pass"},
			// Ignored: a new question is always open and unanswered.
			Status:       TaskQuestionStatusAnswered,
			AnswerOption: "B",
		}
		allowed, err := q.CanCreate(s, u)
		require.NoError(t, err)
		assert.True(t, allowed)
		require.NoError(t, q.Create(s, u))
		require.NoError(t, s.Commit())
		events.DispatchPending(t.Context(), s)

		assert.Equal(t, TaskQuestionStatusOpen, q.Status)
		assert.Equal(t, int64(1), q.Author.ID)
		db.AssertExists(t, "task_questions", map[string]interface{}{
			"id":            q.ID,
			"task_id":       1,
			"author_id":     1,
			"status":        "open",
			"answer_option": "",
		}, false)
		events.AssertDispatched(t, &TaskQuestionCreatedEvent{})
	})
	t.Run("recommendation must be an option", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		q := &TaskQuestion{
			TaskID:         1,
			Title:          "Ship it?",
			Options:        []*TaskQuestionOption{{Key: "A", Label: "Yes"}},
			Recommendation: &TaskQuestionRecommendation{Key: "Z"},
		}
		var invalid ErrInvalidTaskQuestion
		assert.ErrorAs(t, q.Create(s, u), &invalid)
	})
	t.Run("duplicate option keys", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		q := &TaskQuestion{
			TaskID:  1,
			Title:   "Ship it?",
			Options: []*TaskQuestionOption{{Key: "A", Label: "Yes"}, {Key: "A", Label: "No"}},
		}
		var invalid ErrInvalidTaskQuestion
		assert.ErrorAs(t, q.Create(s, u), &invalid)
	})
	t.Run("no write access", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		for _, taskID := range []int64{14, 32} {
			allowed, err := (&TaskQuestion{TaskID: taskID}).CanCreate(s, u)
			require.NoError(t, err)
			assert.False(t, allowed, "task %d", taskID)
		}
	})
	t.Run("write access through a share", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		allowed, err := (&TaskQuestion{TaskID: 19}).CanCreate(s, u)
		require.NoError(t, err)
		assert.True(t, allowed)
	})
}

func TestTaskQuestion_Read(t *testing.T) {
	u := &user.User{ID: 1}

	t.Run("read one with users", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		q := &TaskQuestion{ID: 2, TaskID: 1}
		allowed, _, err := q.CanRead(s, u)
		require.NoError(t, err)
		require.True(t, allowed)
		require.NoError(t, q.ReadOne(s, u))
		assert.Equal(t, "A", q.AnswerOption)
		assert.Equal(t, "for one release", q.AnswerNotes)
		assert.Equal(t, int64(1), q.AnsweredBy.ID)
		assert.Len(t, q.Options, 2)
	})
	t.Run("read-only access can read", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		allowed, _, err := (&TaskQuestion{ID: 3, TaskID: 32}).CanRead(s, u)
		require.NoError(t, err)
		assert.True(t, allowed)
	})
	t.Run("no access", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		allowed, _, err := (&TaskQuestion{ID: 4, TaskID: 14}).CanRead(s, u)
		require.NoError(t, err)
		assert.False(t, allowed)
	})
	t.Run("question of another task", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		_, _, err := (&TaskQuestion{ID: 4, TaskID: 1}).CanRead(s, u)
		assert.True(t, IsErrTaskQuestionDoesNotExist(err))
	})
	t.Run("read all puts open questions first", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		result, _, total, err := (&TaskQuestion{TaskID: 1}).ReadAll(s, u, "", 1, 50)
		require.NoError(t, err)
		questions := result.([]*TaskQuestion)
		require.Len(t, questions, 2)
		assert.Equal(t, int64(2), total)
		assert.Equal(t, int64(1), questions[0].ID)
		assert.Equal(t, "B", questions[0].Recommendation.Key)

		result, _, _, err = (&TaskQuestion{TaskID: 1, Status: TaskQuestionStatusAnswered}).ReadAll(s, u, "", 1, 50)
		require.NoError(t, err)
		require.Len(t, result.([]*TaskQuestion), 1)
	})
	t.Run("read all without access", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		_, _, _, err := (&TaskQuestion{TaskID: 14}).ReadAll(s, u, "", 1, 50)
		assert.True(t, IsErrGenericForbidden(err))
	})
}

func TestTaskQuestion_Answer(t *testing.T) {
	u := &user.User{ID: 1}

	answer := func(t *testing.T, id int64, a *TaskQuestionAnswer) (*TaskQuestion, error) {
		t.Helper()
		s := db.NewSession()
		defer s.Close()
		q := &TaskQuestion{ID: id, TaskID: 1}
		if err := q.Answer(s, u, a); err != nil {
			_ = s.Rollback()
			return q, err
		}
		require.NoError(t, s.Commit())
		events.DispatchPending(t.Context(), s)
		return q, nil
	}

	t.Run("with an option and notes", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		events.Fake()

		q, err := answer(t, 1, &TaskQuestionAnswer{Option: "B", Notes: "go"})
		require.NoError(t, err)
		assert.Equal(t, TaskQuestionStatusAnswered, q.Status)
		assert.Equal(t, int64(1), q.AnsweredBy.ID)
		require.NotNil(t, q.AnsweredAt)
		db.AssertExists(t, "task_questions", map[string]interface{}{
			"id":             1,
			"status":         "answered",
			"answer_option":  "B",
			"answer_notes":   "go",
			"answered_by_id": 1,
		}, false)
		events.AssertDispatched(t, &TaskQuestionAnsweredEvent{})
	})
	t.Run("with other", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		q, err := answer(t, 1, &TaskQuestionAnswer{Other: "neither"})
		require.NoError(t, err)
		assert.Equal(t, "neither", q.AnswerOther)
		assert.Empty(t, q.AnswerOption)
	})
	t.Run("defer, then answer", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		q, err := answer(t, 1, &TaskQuestionAnswer{Defer: true, Notes: "next week"})
		require.NoError(t, err)
		assert.Equal(t, TaskQuestionStatusDeferred, q.Status)

		q, err = answer(t, 1, &TaskQuestionAnswer{Option: "A"})
		require.NoError(t, err)
		assert.Equal(t, TaskQuestionStatusAnswered, q.Status)
	})
	t.Run("invalid answers", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		for name, a := range map[string]*TaskQuestionAnswer{
			"nothing":        {Notes: "just notes"},
			"option + other": {Option: "A", Other: "x"},
			"option + defer": {Option: "A", Defer: true},
			"unknown option": {Option: "Z"},
		} {
			_, err := answer(t, 1, a)
			var invalid ErrInvalidTaskQuestionAnswer
			require.ErrorAs(t, err, &invalid, name)
		}
		db.AssertExists(t, "task_questions", map[string]interface{}{"id": 1, "status": "open"}, false)
	})
	t.Run("already answered", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		_, err := answer(t, 2, &TaskQuestionAnswer{Option: "B"})
		var notOpen ErrTaskQuestionNotOpen
		assert.ErrorAs(t, err, &notOpen)
	})
	t.Run("answering needs write access", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		allowed, err := (&TaskQuestion{ID: 3, TaskID: 32}).CanUpdate(s, u)
		require.NoError(t, err)
		assert.False(t, allowed)
		allowed, err = (&TaskQuestion{ID: 4, TaskID: 14}).CanUpdate(s, u)
		require.NoError(t, err)
		assert.False(t, allowed)
	})
}

func TestTaskQuestion_Update(t *testing.T) {
	u := &user.User{ID: 1}

	update := func(t *testing.T, q *TaskQuestion) error {
		t.Helper()
		s := db.NewSession()
		defer s.Close()
		if err := q.Update(s, u); err != nil {
			_ = s.Rollback()
			return err
		}
		return s.Commit()
	}

	t.Run("edit and cancel", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		q := &TaskQuestion{ID: 1, TaskID: 1, Title: "Which backend, really?", Status: TaskQuestionStatusCancelled}
		require.NoError(t, update(t, q))
		assert.Equal(t, TaskQuestionStatusCancelled, q.Status)
		assert.Equal(t, int64(1), q.Author.ID)
		db.AssertExists(t, "task_questions", map[string]interface{}{
			"id":     1,
			"title":  "Which backend, really?",
			"status": "cancelled",
		}, false)
	})
	t.Run("reopen clears the answer", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		q := &TaskQuestion{ID: 2, TaskID: 1, Title: "Keep the old endpoint?", Status: TaskQuestionStatusOpen}
		require.NoError(t, update(t, q))
		assert.Empty(t, q.AnswerOption)
		assert.Nil(t, q.AnsweredBy)
		db.AssertExists(t, "task_questions", map[string]interface{}{
			"id":             2,
			"status":         "open",
			"answer_option":  "",
			"answered_by_id": 0,
		}, false)
	})
	t.Run("cannot answer through update", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		err := update(t, &TaskQuestion{ID: 1, TaskID: 1, Title: "x", Status: TaskQuestionStatusAnswered})
		var invalid ErrInvalidTaskQuestion
		assert.ErrorAs(t, err, &invalid)
	})
	t.Run("missing question", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		err := update(t, &TaskQuestion{ID: 999, TaskID: 1, Title: "x"})
		assert.True(t, IsErrTaskQuestionDoesNotExist(err))
	})
}

func TestTaskQuestion_Delete(t *testing.T) {
	u := &user.User{ID: 1}

	t.Run("normal", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		q := &TaskQuestion{ID: 1, TaskID: 1}
		allowed, err := q.CanDelete(s, u)
		require.NoError(t, err)
		require.True(t, allowed)
		require.NoError(t, q.Delete(s, u))
		require.NoError(t, s.Commit())
		db.AssertMissing(t, "task_questions", map[string]interface{}{"id": 1})
	})
	t.Run("no access", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		allowed, err := (&TaskQuestion{ID: 4, TaskID: 14}).CanDelete(s, u)
		require.NoError(t, err)
		assert.False(t, allowed)
	})
}

func TestTaskQuestion_Filter(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()
	u := &user.User{ID: 1}

	ids := func(t *testing.T, filter string) []int64 {
		t.Helper()
		tc := &TaskCollection{ProjectID: 0, Filter: filter}
		result, _, _, err := tc.ReadAll(s, u, "", 1, 500)
		require.NoError(t, err)
		out := []int64{}
		for _, task := range result.([]*Task) {
			out = append(out, task.ID)
		}
		return out
	}

	t.Run("open question count", func(t *testing.T) {
		task := &Task{ID: 1}
		require.NoError(t, task.ReadOne(s, u))
		assert.Equal(t, int64(1), task.OpenQuestionCount)
		task = &Task{ID: 2}
		require.NoError(t, task.ReadOne(s, u))
		assert.Zero(t, task.OpenQuestionCount)
	})

	// Task 1 has an open question, task 32 too (read-only share); task 14 is not visible.
	assert.ElementsMatch(t, []int64{1, 32}, ids(t, "questions = open"))
	assert.ElementsMatch(t, []int64{1}, ids(t, "questions = answered"))
	assert.NotContains(t, ids(t, "questions != open"), int64(1))

	_, _, _, err := (&TaskCollection{Filter: "questions = bogus"}).ReadAll(s, u, "", 1, 50)
	require.Error(t, err)
	for _, filter := range []string{"questions like open", "questions > open"} {
		_, _, _, err = (&TaskCollection{Filter: filter}).ReadAll(s, u, "", 1, 50)
		assert.True(t, IsErrInvalidTaskFilterValue(err), "%s: %v", filter, err)
	}
	assert.ElementsMatch(t, []int64{1, 32}, ids(t, "questions in open, deferred"))
}

func TestTaskQuestion_AnswerReachesWebhooks(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	u := &user.User{ID: 1}
	s := db.NewSession()
	defer s.Close()

	hook := &Webhook{
		TargetURL:   "https://example.com/answered",
		Events:      []string{"task.question.answered"},
		ProjectID:   1,
		CreatedByID: 1,
	}
	_, err := s.Insert(hook)
	require.NoError(t, err)
	require.NoError(t, s.Commit())

	events.ClearDispatchedEvents()
	q := &TaskQuestion{ID: 1, TaskID: 1}
	require.NoError(t, q.Answer(s, u, &TaskQuestionAnswer{Option: "B", Notes: "ship S3"}))
	require.NoError(t, s.Commit())
	events.DispatchPending(context.Background(), s)
	answered := events.GetDispatchedEvents((&TaskQuestionAnsweredEvent{}).Name())
	require.Len(t, answered, 1)

	events.ClearDispatchedEvents()
	events.TestListener(t, answered[0], &WebhookListener{EventName: "task.question.answered"})

	delivered := webhookDeliveriesByID(t)
	require.Contains(t, delivered, hook.ID)
	raw, err := json.Marshal(delivered[hook.ID].Payload)
	require.NoError(t, err)
	var payload struct {
		EventName string `json:"event_name"`
		Data      struct {
			Question struct {
				ID           int64  `json:"id"`
				Status       string `json:"status"`
				AnswerOption string `json:"answer_option"`
				AnswerNotes  string `json:"answer_notes"`
			} `json:"question"`
			Task struct {
				ID int64 `json:"id"`
			} `json:"task"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &payload))
	assert.Equal(t, "task.question.answered", payload.EventName)
	assert.Equal(t, int64(1), payload.Data.Task.ID)
	assert.Equal(t, "answered", payload.Data.Question.Status)
	assert.Equal(t, "B", payload.Data.Question.AnswerOption)
	assert.Equal(t, "ship S3", payload.Data.Question.AnswerNotes)
}
