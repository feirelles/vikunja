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

package webtests

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"code.vikunja.io/api/pkg/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHumaTaskQuestion covers the task question routes. Task 1 belongs to
// testuser1; testuser6 cannot see project 1. Subtests share one fixture load
// and are ordered so they do not clobber each other's rows.
func TestHumaTaskQuestion(t *testing.T) {
	onTask1 := webHandlerTestV2{
		user:     &testuser1,
		basePath: "/api/v2/tasks/1/questions",
		idParam:  "question",
		t:        t,
	}
	require.NoError(t, onTask1.ensureEnv())
	asUser6 := &webHandlerTestV2{
		user:     &testuser6,
		basePath: "/api/v2/tasks/1/questions",
		idParam:  "question",
		t:        t,
		e:        onTask1.e,
	}
	onTask2 := &webHandlerTestV2{
		user:     &testuser1,
		basePath: "/api/v2/tasks/2/questions",
		idParam:  "question",
		t:        t,
		e:        onTask1.e,
	}
	token := humaTokenFor(t, &testuser1)
	answer := func(id, body string) *http.Response {
		rec := humaRequest(t, onTask1.e, http.MethodPost, "/api/v2/tasks/1/questions/"+id+"/answer", body, token, "")
		return rec.Result()
	}

	t.Run("ReadAll", func(t *testing.T) {
		rec, err := onTask1.testReadAllWithUser(nil, nil)
		require.NoError(t, err)
		var page struct {
			Items []struct {
				ID             int64  `json:"id"`
				Status         string `json:"status"`
				Recommendation *struct {
					Key string `json:"key"`
				} `json:"recommendation"`
			} `json:"items"`
			Total int64 `json:"total"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
		require.Len(t, page.Items, 2)
		assert.Equal(t, "open", page.Items[0].Status)
		assert.Equal(t, "B", page.Items[0].Recommendation.Key)

		rec, err = onTask1.testReadAllWithUser(url.Values{"status": []string{"answered"}}, nil)
		require.NoError(t, err)
		assert.Contains(t, rec.Body.String(), `Keep the old endpoint?`)
		assert.NotContains(t, rec.Body.String(), `Which storage backend?`)

		_, err = asUser6.testReadAllWithUser(nil, nil)
		assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
	})

	t.Run("ReadOne", func(t *testing.T) {
		rec, err := onTask1.testReadOneWithUser(nil, map[string]string{"question": "2"})
		require.NoError(t, err)
		assert.Contains(t, rec.Body.String(), `"answer_option":"A"`)
		assert.Contains(t, rec.Body.String(), `"max_permission":`)
		assert.NotEmpty(t, rec.Result().Header.Get("ETag"))

		_, err = onTask2.testReadOneWithUser(nil, map[string]string{"question": "1"})
		assert.Equal(t, http.StatusNotFound, getHTTPErrorCode(err))
		_, err = asUser6.testReadOneWithUser(nil, map[string]string{"question": "1"})
		assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
	})

	var created struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	t.Run("Create", func(t *testing.T) {
		rec, err := onTask1.testCreateWithUser(nil, nil, `{"title":"Rename it?","options":[{"key":"A","label":"Yes"},{"key":"B","label":"No"}],"recommendation":{"key":"A","reason":"clearer"},"status":"answered"}`)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, rec.Code)
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		assert.Equal(t, "open", created.Status)

		_, err = onTask1.testCreateWithUser(nil, nil, `{"title":"Bad","options":[{"key":"A","label":"Yes"}],"recommendation":{"key":"Z"}}`)
		assert.Equal(t, http.StatusBadRequest, getHTTPErrorCode(err))
		_, err = asUser6.testCreateWithUser(nil, nil, `{"title":"Not mine"}`)
		assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
	})

	t.Run("Answer", func(t *testing.T) {
		id := json.Number(strconv.FormatInt(created.ID, 10))
		res := answer(id.String(), `{"option":"B","notes":"keep it short"}`)
		require.Equal(t, http.StatusOK, res.StatusCode)
		db.AssertExists(t, "task_questions", map[string]interface{}{
			"id":            created.ID,
			"status":        "answered",
			"answer_option": "B",
			"answer_notes":  "keep it short",
		}, false)

		assert.Equal(t, http.StatusPreconditionFailed, answer(id.String(), `{"option":"A"}`).StatusCode)
		assert.Equal(t, http.StatusBadRequest, answer("1", `{"option":"Z"}`).StatusCode)
		assert.Equal(t, http.StatusBadRequest, answer("1", `{"option":"A","other":"x"}`).StatusCode)
		assert.Equal(t, http.StatusNotFound, answer("999", `{"option":"A"}`).StatusCode)

		rec := humaRequest(t, onTask1.e, http.MethodPost, "/api/v2/tasks/1/questions/1/answer", `{"defer":true}`, humaTokenFor(t, &testuser6), "")
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("Update", func(t *testing.T) {
		rec := humaRequest(t, onTask1.e, http.MethodPatch, "/api/v2/tasks/1/questions/2", `{"status":"open"}`, token, "application/merge-patch+json")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"status":"open"`)
		assert.Contains(t, rec.Body.String(), `"answer_option":""`)

		rec = humaRequest(t, onTask1.e, http.MethodPatch, "/api/v2/tasks/1/questions/2", `{"status":"answered"}`, token, "application/merge-patch+json")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("Delete", func(t *testing.T) {
		_, err := asUser6.testDeleteWithUser(nil, map[string]string{"question": "1"})
		assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		_, err = onTask1.testDeleteWithUser(nil, map[string]string{"question": "1"})
		require.NoError(t, err)
		db.AssertMissing(t, "task_questions", map[string]interface{}{"id": 1})
	})
}
