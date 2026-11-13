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

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"code.vikunja.io/veans/internal/client"
	"code.vikunja.io/veans/internal/config"
)

// fakeClaimServer models the slice of Vikunja that `veans claim` touches:
// task 42 (index 6 in project 7) with a mutable label set, and a global
// label list that grows when claim creates a label.
type fakeClaimServer struct {
	t        *testing.T
	mu       sync.Mutex
	labels   map[int64]string // all labels the bot can see: id → title
	onTask   map[int64]bool   // label ids attached to task 42
	nextID   int64
	assigned bool // bot (user 5) assigned to task 42
	calls    []string
}

func newFakeClaimServer(t *testing.T, labels map[int64]string, onTask ...int64) *fakeClaimServer {
	f := &fakeClaimServer{t: t, labels: labels, onTask: map[int64]bool{}, nextID: 100}
	for _, id := range onTask {
		f.onTask[id] = true
	}
	return f
}

func (f *fakeClaimServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/projects/7/tasks":
		_, _ = w.Write([]byte(`{"items":[{"id":42,"index":6,"project_id":7}],"total":1,"page":1,"per_page":50,"total_pages":1}`))
	case r.Method == http.MethodPut && r.URL.Path == "/api/v2/projects/7/views/1/buckets/11/tasks":
		_, _ = w.Write([]byte(`{"task_id":42}`))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v2/tasks/42/assignees":
		if f.assigned {
			// What Vikunja really answers on a repeated assign.
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"title":"This user is already assigned to that task."}`))
			return
		}
		f.assigned = true
		_, _ = w.Write([]byte(`{"user_id":5}`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/tasks/42":
		task := client.Task{ID: 42, Index: 6, ProjectID: 7}
		if f.assigned {
			task.Assignees = []*client.User{{ID: 5, Username: "bot-vikunja"}}
		}
		for _, id := range f.sortedOnTask() {
			task.Labels = append(task.Labels, &client.Label{ID: id, Title: f.labels[id]})
		}
		f.writeJSON(w, task)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/labels":
		q := r.URL.Query().Get("q")
		page := client.Paginated[*client.Label]{Items: []*client.Label{}, Page: 1, PerPage: 50, TotalPages: 1}
		for id, title := range f.labels {
			if strings.Contains(title, q) {
				page.Items = append(page.Items, &client.Label{ID: id, Title: title})
			}
		}
		page.Total = int64(len(page.Items))
		f.writeJSON(w, page)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v2/labels":
		var body client.Label
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.nextID++
		f.labels[f.nextID] = body.Title
		f.writeJSON(w, client.Label{ID: f.nextID, Title: body.Title})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v2/tasks/42/labels":
		var body client.LabelTask
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.onTask[body.LabelID] = true
		f.writeJSON(w, body)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v2/tasks/42/labels/"):
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v2/tasks/42/labels/"), 10, 64)
		if err != nil || !f.onTask[id] {
			http.Error(w, `{"title":"not found"}`, http.StatusNotFound)
			return
		}
		delete(f.onTask, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		f.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}
}

func (f *fakeClaimServer) writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		f.t.Errorf("encode response: %v", err)
	}
}

func (f *fakeClaimServer) sortedOnTask() []int64 {
	ids := make([]int64, 0, len(f.onTask))
	for id := range f.onTask {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (f *fakeClaimServer) taskLabelTitles() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, id := range f.sortedOnTask() {
		out = append(out, f.labels[id])
	}
	sort.Strings(out)
	return out
}

func (f *fakeClaimServer) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func claim(t *testing.T, f *fakeClaimServer, branch string, explicit bool) *client.Task {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	rt := &runtime{
		cfg: &config.Config{
			Server: srv.URL, ProjectID: 7, ProjectIdentifier: "VKJ", ViewID: 1,
			Bot:     config.Bot{Username: "bot-vikunja", UserID: 5},
			Buckets: config.Buckets{Todo: 10, InProgress: 11, InReview: 12, Done: 13, Scrapped: 14},
		},
		client: client.New(srv.URL, "tk"),
	}
	var buf bytes.Buffer
	if err := runClaim(context.Background(), rt, &buf, "VKJ-6", branch, explicit); err != nil {
		t.Fatalf("runClaim: %v", err)
	}
	var task client.Task
	if err := json.Unmarshal(buf.Bytes(), &task); err != nil {
		t.Fatalf("stdout is not a task: %v\n%s", err, buf.String())
	}
	return &task
}

func assertLabels(t *testing.T, f *fakeClaimServer, want ...string) {
	t.Helper()
	sort.Strings(want)
	got := f.taskLabelTitles()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("labels on task:\nwant %v\ngot  %v", want, got)
	}
}

// Without --branch, claim keeps its old behaviour: it adds the checkout's
// branch label and leaves other branch labels alone.
func TestClaim_NoFlagAddsDetectedBranchAndKeepsOthers(t *testing.T) {
	f := newFakeClaimServer(t, map[int64]string{1: "veans:branch:old", 2: "veans:bug"}, 1, 2)
	task := claim(t, f, "feat/detected", false)

	assertLabels(t, f, "veans:branch:old", "veans:bug", "veans:branch:feat/detected")
	if n := f.count("DELETE "); n != 0 {
		t.Fatalf("no label may be removed without --branch, got %d DELETEs", n)
	}
	if f.count("PUT /api/v2/projects/7/views/1/buckets/11/tasks") != 1 || f.count("POST /api/v2/tasks/42/assignees") != 1 {
		t.Fatalf("claim must move to In Progress and assign the bot: %v", f.calls)
	}
	if task.ID != 42 || len(task.Labels) != 3 {
		t.Fatalf("unexpected output task %+v", task)
	}
}

// Without --branch and outside a git checkout no branch label is touched.
func TestClaim_NoFlagNoBranchSkipsLabel(t *testing.T) {
	f := newFakeClaimServer(t, map[int64]string{})
	claim(t, f, "", false)
	assertLabels(t, f)
	if n := f.count("POST /api/v2/labels") + f.count("POST /api/v2/tasks/42/labels"); n != 0 {
		t.Fatalf("expected no label calls, got %v", f.calls)
	}
}

func TestClaim_FlagCreatesNamedBranchLabel(t *testing.T) {
	f := newFakeClaimServer(t, map[int64]string{2: "veans:bug"}, 2)
	claim(t, f, "feat/task-branch", true)

	assertLabels(t, f, "veans:bug", "veans:branch:feat/task-branch")
	if f.count("POST /api/v2/labels") != 1 {
		t.Fatalf("expected the label to be created once: %v", f.calls)
	}
}

func TestClaim_FlagReusesExistingLabel(t *testing.T) {
	f := newFakeClaimServer(t, map[int64]string{3: "veans:branch:feat/task-branch"})
	claim(t, f, "feat/task-branch", true)

	assertLabels(t, f, "veans:branch:feat/task-branch")
	if f.count("POST /api/v2/labels") != 0 {
		t.Fatalf("existing label must be reused, not created: %v", f.calls)
	}
}

// The case VKJ-6 is about: the task carries the label of the session's
// previous branch; claiming with --branch swaps it for the right one and
// keeps the non-branch labels.
func TestClaim_FlagReplacesOtherBranchLabels(t *testing.T) {
	f := newFakeClaimServer(t, map[int64]string{
		1: "veans:branch:wrong-one",
		4: "veans:branch:wrong-two",
		2: "veans:bug",
	}, 1, 4, 2)
	claim(t, f, "feat/task-branch", true)

	assertLabels(t, f, "veans:bug", "veans:branch:feat/task-branch")
	if n := f.count("DELETE /api/v2/tasks/42/labels/"); n != 2 {
		t.Fatalf("expected 2 stale branch labels removed, got %d: %v", n, f.calls)
	}
}

// Repeating a claim with the label already in place is a no-op for labels.
func TestClaim_FlagAlreadyTaggedIsNoop(t *testing.T) {
	f := newFakeClaimServer(t, map[int64]string{3: "veans:branch:feat/task-branch"}, 3)
	claim(t, f, "feat/task-branch", true)

	assertLabels(t, f, "veans:branch:feat/task-branch")
	if n := f.count("POST /api/v2/tasks/42/labels") + f.count("DELETE "); n != 0 {
		t.Fatalf("expected no label writes, got %v", f.calls)
	}
}

// A repeated claim (bot already assigned) must not trip over Vikunja's 400
// "already assigned" and must still fix the branch label.
func TestClaim_ReclaimSkipsAssignAndFixesLabel(t *testing.T) {
	f := newFakeClaimServer(t, map[int64]string{1: "veans:branch:wrong"}, 1)
	f.assigned = true
	claim(t, f, "feat/task-branch", true)

	assertLabels(t, f, "veans:branch:feat/task-branch")
	if n := f.count("POST /api/v2/tasks/42/assignees"); n != 0 {
		t.Fatalf("already-assigned bot must not be re-assigned, got %d POSTs", n)
	}
}

func TestClaimCmd_EmptyBranchFlagRejected(t *testing.T) {
	cmd := newClaimCmd()
	cmd.SetArgs([]string{"VKJ-6", "--branch", "  "})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	wantCode(t, err, "VALIDATION_ERROR")
}
