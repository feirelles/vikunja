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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeRelServer answers the endpoints create/update/relate touch and keeps
// the bodies it got. Task index N resolves to id 100+N; a created task gets
// id 200. A relation to task 999 answers 409, as Vikunja does for a relation
// that already exists.
type fakeRelServer struct {
	t     *testing.T
	mu    sync.Mutex
	calls []string
	posts map[string][]map[string]any // path → bodies
}

func newFakeRelServer(t *testing.T) (*fakeRelServer, *runtime) {
	f := &fakeRelServer{t: t, posts: map[string][]map[string]any{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	rt := newTestRuntime(srv.URL)
	rt.cfg.ProjectIdentifier = "VKJ"
	return f, rt
}

func (f *fakeRelServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	var body map[string]any
	if r.Method == http.MethodPost || r.Method == http.MethodPatch {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.posts[r.Method+" "+r.URL.Path] = append(f.posts[r.Method+" "+r.URL.Path], body)
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/projects/7/tasks":
		var idx int64
		_, _ = fmt.Sscanf(r.URL.Query().Get("filter"), "index = %d", &idx)
		fmt.Fprintf(w, `{"items":[{"id":%d,"index":%d,"project_id":7}],"total":1,"page":1,"per_page":50,"total_pages":1}`, 100+idx, idx)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v2/projects/7/tasks":
		_, _ = w.Write([]byte(`{"id":200,"index":30,"project_id":7}`))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/relations"):
		if body["other_task_id"] == float64(999) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"title":"The task relation already exists."}`))
			return
		}
		fmt.Fprintf(w, `{"task_id":1,"other_task_id":%v,"relation_kind":%q}`, body["other_task_id"], body["relation_kind"])
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v2/tasks/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/v2/tasks/")
		fmt.Fprintf(w, `{"id":%s,"index":30,"project_id":7,"description":"<p>old</p>","updated":"2026-01-01T00:00:00Z"}`, id)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments"):
		_, _ = w.Write([]byte(`{"id":1}`))
	case r.Method == http.MethodPatch:
		_, _ = w.Write([]byte(`{"id":42}`))
	default:
		f.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}
}

func (f *fakeRelServer) bodies(key string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.posts[key]
}

func TestRelate_CreatesRelation(t *testing.T) {
	f, rt := newFakeRelServer(t)
	res, existed, err := rt.relate(context.Background(), 117, 116, "blocking")
	if err != nil || existed {
		t.Fatalf("relate: %v existed=%v", err, existed)
	}
	got := f.bodies("POST /api/v2/tasks/117/relations")
	if len(got) != 1 || got[0]["other_task_id"] != float64(116) || got[0]["relation_kind"] != "blocking" {
		t.Fatalf("relation bodies: %v", got)
	}
	if res.TaskID != 117 || res.OtherTaskID != 116 || res.RelationKind != "blocking" {
		t.Fatalf("result: %+v", res)
	}
}

func TestRelate_ExistingIsSuccess(t *testing.T) {
	_, rt := newFakeRelServer(t)
	_, existed, err := rt.relate(context.Background(), 117, 999, "related")
	if err != nil || !existed {
		t.Fatalf("want existed without error, got existed=%v err=%v", existed, err)
	}
}

func TestRelate_SelfRejected(t *testing.T) {
	_, rt := newFakeRelServer(t)
	_, _, err := rt.relate(context.Background(), 5, 5, "related")
	wantCode(t, err, "VALIDATION_ERROR")
}

func TestValidRelationKind(t *testing.T) {
	for _, k := range []string{"related", "blocking", "subtask"} {
		if err := validRelationKind(k); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
	wantCode(t, validRelationKind("friends"), "VALIDATION_ERROR")
}

func TestResolveRelationTarget(t *testing.T) {
	_, rt := newFakeRelServer(t)
	ctx := context.Background()
	cases := map[string]int64{
		"VKJ-16":                               116,
		"#3":                                   103,
		"https://tasks.example.com/tasks/540":  540,
		"https://tasks.example.com/tasks/540/": 540,
	}
	for in, want := range cases {
		got, err := rt.resolveRelationTarget(ctx, in)
		if err != nil || got != want {
			t.Errorf("%s → %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"https://tasks.example.com/projects/2", "https://x/tasks/abc", "TASKS-14"} {
		if _, err := rt.resolveRelationTarget(ctx, bad); err == nil {
			t.Errorf("%s: want error", bad)
		}
	}
}

// VKJ-17: a derived task with its relation is one command, no JSON needed.
func TestCreate_RelatedToAndMarkdown(t *testing.T) {
	f, rt := newFakeRelServer(t)
	_, err := runCreate(context.Background(), rt, "derived", &createFlags{
		statusName:  "todo",
		description: "Found while doing **VKJ-16**.\n\n- [ ] check it",
		md:          true,
		relatedTo:   []string{"VKJ-16", "https://tasks.example.com/tasks/540"},
	})
	if err != nil {
		t.Fatalf("runCreate: %v", err)
	}
	created := f.bodies("POST /api/v2/projects/7/tasks")
	wantDesc := "<p>Found while doing <strong>VKJ-16</strong>.</p>\n<ul data-type=\"taskList\">\n<li data-type=\"taskItem\" data-checked=\"false\"><p>check it</p></li>\n</ul>"
	if len(created) != 1 || created[0]["description"] != wantDesc {
		t.Fatalf("create body: %v", created)
	}
	rels := f.bodies("POST /api/v2/tasks/200/relations")
	if len(rels) != 2 || rels[0]["other_task_id"] != float64(116) || rels[1]["other_task_id"] != float64(540) {
		t.Fatalf("relations: %v", rels)
	}
	for _, r := range rels {
		if r["relation_kind"] != "related" {
			t.Fatalf("kind: %v", r)
		}
	}
}

// A bad --related-to fails before the task exists.
func TestCreate_BadRelatedToCreatesNothing(t *testing.T) {
	f, rt := newFakeRelServer(t)
	_, err := runCreate(context.Background(), rt, "derived", &createFlags{
		statusName: "todo",
		relatedTo:  []string{"https://tasks.example.com/nope"},
	})
	wantCode(t, err, "VALIDATION_ERROR")
	if n := len(f.bodies("POST /api/v2/projects/7/tasks")); n != 0 {
		t.Fatalf("task created despite bad --related-to: %v", f.calls)
	}
}

// Without --md the description is sent as given.
func TestCreate_NoMarkdownKeepsText(t *testing.T) {
	f, rt := newFakeRelServer(t)
	if _, err := runCreate(context.Background(), rt, "t", &createFlags{statusName: "todo", description: "**raw**"}); err != nil {
		t.Fatal(err)
	}
	if got := f.bodies("POST /api/v2/projects/7/tasks")[0]["description"]; got != "**raw**" {
		t.Fatalf("description = %v", got)
	}
}

func TestUpdate_MarkdownCommentAndAppend(t *testing.T) {
	f, rt := newFakeRelServer(t)
	_, err := runUpdate(context.Background(), rt, 42, &updateFlags{
		comment:        "Done: `veans pr`\n\n- one",
		descriptionApp: "## Notes",
		md:             true,
	})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	comments := f.bodies("POST /api/v2/tasks/42/comments")
	if len(comments) != 1 || comments[0]["comment"] != "<p>Done: <code>veans pr</code></p>\n<ul>\n<li><p>one</p></li>\n</ul>" {
		t.Fatalf("comment: %v", comments)
	}
	patch := f.bodies("PATCH /api/v2/tasks/42")
	if len(patch) != 1 || patch[0]["description"] != "<p>old</p>\n<h2>Notes</h2>" {
		t.Fatalf("patch: %v", patch)
	}
}

// --description-replace-old/new match stored HTML, so --md leaves them alone.
func TestUpdate_MarkdownLeavesReplaceAlone(t *testing.T) {
	f, rt := newFakeRelServer(t)
	_, err := runUpdate(context.Background(), rt, 42, &updateFlags{
		replaceOld: "<p>old</p>", replaceNew: "<p>**new**</p>", md: true,
	})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	if got := f.bodies("PATCH /api/v2/tasks/42")[0]["description"]; got != "<p>**new**</p>" {
		t.Fatalf("description = %v", got)
	}
}
