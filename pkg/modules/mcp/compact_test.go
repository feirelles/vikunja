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

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	apiv2 "code.vikunja.io/api/pkg/routes/api/v2"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleTask() map[string]any {
	return map[string]any{
		"id":          1,
		"identifier":  "PROJ-1",
		"index":       1,
		"title":       "t",
		"description": "<p>long</p>",
		"done":        false,
		"project_id":  2,
		"bucket_id":   3,
		"due_date":    "2030-01-01T00:00:00Z",
		"priority":    4,
		"created_by":  map[string]any{"id": 1, "username": "u", "email": "x"},
		"labels": []any{
			map[string]any{"id": 5, "title": "bug", "hex_color": "ff0000"},
		},
		"positions": []any{map[string]any{"project_view_id": 1, "position": 2.5}},
	}
}

func TestCompactValue_Task(t *testing.T) {
	want := map[string]any{
		"id":         1,
		"identifier": "PROJ-1",
		"title":      "t",
		"done":       false,
		"project_id": 2,
		"bucket_id":  3,
		"due_date":   "2030-01-01T00:00:00Z",
		"labels":     []any{map[string]any{"title": "bug"}},
	}
	assert.Equal(t, want, compactValue(sampleTask(), compactWrite))
	assert.Equal(t, []any{want}, compactValue([]any{sampleTask()}, compactList))
}

func TestCompactValue_NestedTaskIsSummarized(t *testing.T) {
	// Placing a task in a bucket returns the bucket and the whole task.
	got := compactValue(map[string]any{
		"task_id":         1,
		"bucket_id":       3,
		"project_view_id": 4,
		"bucket":          map[string]any{"id": 3, "title": "Done", "count": 7, "created_by": map[string]any{"id": 1}},
		"task":            sampleTask(),
	}, compactWrite).(map[string]any)
	assert.Equal(t, map[string]any{"id": 3, "title": "Done"}, got["bucket"])
	assert.Equal(t, "PROJ-1", got["task"].(map[string]any)["identifier"])
	assert.NotContains(t, got["task"], "description")
	assert.Equal(t, 4, got["project_view_id"])
}

func TestCompactValue_RichText(t *testing.T) {
	long := strings.Repeat("é", listRichTextLimit+10)
	comment := map[string]any{
		"id":      9,
		"comment": long,
		"author":  map[string]any{"id": 1, "username": "u", "name": "U", "email": "x"},
		"created": "2030-01-01T00:00:00Z",
	}

	written := compactValue(comment, compactWrite).(map[string]any)
	assert.NotContains(t, written, "comment")
	assert.Equal(t, map[string]any{"id": 1, "username": "u", "name": "U"}, written["author"])

	// A listed comment stays whole; only descriptions are cut.
	listed := compactValue(comment, compactList).(map[string]any)
	assert.Equal(t, long, listed["comment"])

	project := compactValue(map[string]any{"id": 1, "description": long}, compactList).(map[string]any)
	assert.Equal(t, strings.Repeat("é", listRichTextLimit)+"…", project["description"])

	short := compactValue(map[string]any{"description": "short"}, compactList).(map[string]any)
	assert.Equal(t, "short", short["description"])
}

func TestCompactValue_DropsArraysOfNonTasks(t *testing.T) {
	got := compactValue(map[string]any{"id": 1, "title": "p", "views": []any{map[string]any{"id": 2}}}, compactWrite)
	assert.Equal(t, map[string]any{"id": 1, "title": "p"}, got)
}

func TestCallTool_WritesAreCompactByDefault(t *testing.T) {
	m, ctx := newTestCaller(t)
	res, err := m.callTool(ctx, "things_update", json.RawMessage(`{"id":7,"done":true}`))
	require.NoError(t, err)
	out := echoResult(t, res)
	assert.Equal(t, http.MethodPut, out["method"])
	// The echoed body is a nested object: only its id and title remain.
	assert.Equal(t, map[string]any{"id": json.Number("7"), "title": "stored"}, out["body"])
}

func TestCallTool_WriteFieldsAreHonored(t *testing.T) {
	m, ctx := newTestCaller(t)
	res, err := m.callTool(ctx, "things_create", json.RawMessage(`{"title":"hi","fields":["body.title","body.description"]}`))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"body": map[string]any{"title": "hi", "description": ""}}, res)

	res, err = m.callTool(ctx, "things_create", json.RawMessage(`{"title":"hi","description":"d","fields":["*"]}`))
	require.NoError(t, err)
	assert.Equal(t, "d", echoResult(t, res)["body"].(map[string]any)["description"])
}

func TestCallTool_SingleReadIsComplete(t *testing.T) {
	m, ctx := newTestCaller(t)
	res, err := m.callTool(ctx, "things_read", json.RawMessage(`{"id":7}`))
	require.NoError(t, err)
	out := echoResult(t, res)
	assert.Equal(t, "keep me", out["description"])
	assert.Equal(t, map[string]any{"id": json.Number("1"), "name": ""}, out["owner"])
}

func TestCallTool_ListsAreCompactByDefault(t *testing.T) {
	cfg := huma.DefaultConfig("test", "1")
	cfg.FieldsOptionalByDefault = true
	_, api := humatest.New(t, cfg)
	long := strings.Repeat("a", listRichTextLimit+1)
	huma.Register(api, huma.Operation{
		OperationID: "things-page",
		Method:      http.MethodGet,
		Path:        "/things",
	}, func(_ context.Context, _ *struct{}) (*struct{ Body apiv2.Paginated[testThing] }, error) {
		out := &struct{ Body apiv2.Paginated[testThing] }{}
		out.Body = apiv2.NewPaginated([]testThing{{
			ID:          1,
			Title:       "a",
			Description: long,
			Owner:       &testOwner{ID: 2, Name: "n"},
			Reminders:   []*testWhen{{At: "2030-01-01T00:00:00Z"}},
		}}, 1, 1, 50)
		return out, nil
	})
	mod, err := New(api, nil)
	require.NoError(t, err)
	m, ctx := newTestCallerFor(t, mod)

	res, err := m.callTool(ctx, "things_page", json.RawMessage(`{}`))
	require.NoError(t, err)
	out := echoResult(t, res)
	assert.Equal(t, []any{map[string]any{
		"id":          json.Number("1"),
		"title":       "a",
		"description": long[:listRichTextLimit] + "…",
		"done":        false,
		"owner":       map[string]any{"id": json.Number("2"), "name": "n"},
	}}, out["items"])
	assert.Equal(t, json.Number("1"), out["total"])

	res, err = m.callTool(ctx, "things_page", json.RawMessage(`{"fields":["*"]}`))
	require.NoError(t, err)
	item := echoResult(t, res)["items"].([]any)[0].(map[string]any)
	assert.Equal(t, long, item["description"])
	assert.Len(t, item["reminders"], 1)
}
