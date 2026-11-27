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
	"encoding/json"
	"strings"
	"testing"

	"code.vikunja.io/veans/internal/client"
	"code.vikunja.io/veans/internal/config"
)

func renderRuntime() *runtime {
	return &runtime{cfg: &config.Config{
		Server: "https://vikunja.example.com", FrontendURL: "https://tasks.example.com/",
		ProjectID: 7, ProjectIdentifier: "VKJ", ViewID: 1,
		Buckets: config.Buckets{Todo: 10, InProgress: 11, InReview: 12, Done: 13, Scrapped: 14},
	}}
}

func renderTask() *client.Task {
	return &client.Task{
		ID: 42, Index: 6, ProjectID: 7, Title: "short output", Priority: 3,
		Description: "<p>long body</p>",
		Buckets:     []*client.Bucket{{ID: 12, ProjectViewID: 1}},
		Labels:      []*client.Label{{ID: 1, Title: "veans:derived", Description: "<p>long label text</p>"}},
		Assignees:   []*client.User{{ID: 5, Username: "bot-vikunja", Name: "veans bot"}},
		RelatedTasks: map[string][]*client.Task{
			"related": {{ID: 50, Index: 9, ProjectID: 7}, {ID: 77, ProjectID: 3, Identifier: "TASKS-4"}},
		},
	}
}

func TestEmitTask_ShortLine(t *testing.T) {
	rt := renderRuntime()
	var buf bytes.Buffer
	if err := rt.emitTask(&buf, renderTask(), false, "updated (status, comment)"); err != nil {
		t.Fatal(err)
	}
	want := "VKJ-6 updated (status, comment) · In Review · " + rt.cfg.TaskURL(42) + "\n"
	if buf.String() != want {
		t.Fatalf("want %q\ngot  %q", want, buf.String())
	}
}

// A task without bucket info (e.g. straight from PATCH) drops the status part.
func TestEmitTask_ShortLineWithoutBucket(t *testing.T) {
	rt := renderRuntime()
	task := renderTask()
	task.Buckets = nil
	var buf bytes.Buffer
	if err := rt.emitTask(&buf, task, false, "created"); err != nil {
		t.Fatal(err)
	}
	if want := "VKJ-6 created · " + rt.cfg.TaskURL(42) + "\n"; buf.String() != want {
		t.Fatalf("want %q\ngot  %q", want, buf.String())
	}
}

func TestEmitTask_JSONIsFullTask(t *testing.T) {
	var buf bytes.Buffer
	if err := renderRuntime().emitTask(&buf, renderTask(), true, "created"); err != nil {
		t.Fatal(err)
	}
	var got client.Task
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not a task: %v\n%s", err, buf.String())
	}
	if got.ID != 42 || got.Description != "<p>long body</p>" || got.Labels[0].Description == "" || got.URL == "" {
		t.Fatalf("full task lost fields: %+v", got)
	}
}

func TestSummarize(t *testing.T) {
	rt := renderRuntime()
	task := renderTask()
	rt.withURLs(task)

	s := rt.summarize(task, true)
	if s.Identifier != "VKJ-6" || s.Status != "in-review" || s.Priority != 3 || s.URL == "" {
		t.Fatalf("summary header: %+v", s)
	}
	if strings.Join(s.Labels, ",") != "veans:derived" || strings.Join(s.Assignees, ",") != "bot-vikunja" {
		t.Fatalf("labels/assignees: %+v", s)
	}
	if got := strings.Join(s.Related["related"], ","); got != "VKJ-9,TASKS-4" {
		t.Fatalf("related = %q", got)
	}
	if s.Description == "" {
		t.Fatal("show summary must keep the description")
	}

	b, err := json.Marshal(rt.summarize(task, false))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "long body") || strings.Contains(string(b), "long label text") || strings.Contains(string(b), "related") {
		t.Fatalf("list summary leaks heavy fields: %s", b)
	}
}

// A task with no labels must still encode labels as [] for JSON consumers.
func TestSummarize_EmptyLabelsIsArray(t *testing.T) {
	b, err := json.Marshal(renderRuntime().summarize(&client.Task{ID: 1, Index: 1}, false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"labels":[]`) {
		t.Fatalf("labels not an empty array: %s", b)
	}
}

func TestSelectFields(t *testing.T) {
	rt := renderRuntime()
	m, err := rt.selectFields(renderTask(), []string{"title", "status", "labels", "project_id"})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 4 || m["title"] != "short output" || m["status"] != "in-review" || m["project_id"] != float64(7) {
		t.Fatalf("fields: %#v", m)
	}
	if labels, ok := m["labels"].([]any); !ok || labels[0] != "veans:derived" {
		t.Fatalf("labels should be titles: %#v", m["labels"])
	}

	if _, err := rt.selectFields(renderTask(), []string{"nope"}); err == nil {
		t.Fatal("unknown field must fail")
	} else {
		wantCode(t, err, "VALIDATION_ERROR")
	}
}

func TestUpdateVerb(t *testing.T) {
	cases := []struct {
		f    updateFlags
		want string
	}{
		{updateFlags{}, "unchanged"},
		{updateFlags{comment: "x"}, "updated (comment)"},
		{updateFlags{statusName: "in-review", comment: "x", addLabels: []string{"a"}}, "updated (status, comment, labels)"},
		{updateFlags{descriptionApp: "x", priorityIsSet: true}, "updated (priority, description)"},
	}
	for _, tc := range cases {
		if got := updateVerb(&tc.f); got != tc.want {
			t.Errorf("updateVerb(%+v) = %q, want %q", tc.f, got, tc.want)
		}
	}
}

// --json is persistent on the root so every subcommand accepts it.
func TestRoot_JSONFlagIsPersistent(t *testing.T) {
	root := Root("test")
	for _, name := range []string{"create", "update", "claim", "show", "list"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		if cmd.Flags().Lookup(jsonFlag) == nil && cmd.InheritedFlags().Lookup(jsonFlag) == nil {
			t.Errorf("%s does not accept --json", name)
		}
	}
}

// The board's own bucket title wins: a status may map to a bucket named
// differently (todo → "Backlog"), and extra buckets have no status at all.
func TestBucketTitles(t *testing.T) {
	rt := renderRuntime()
	task := renderTask()

	task.Buckets = []*client.Bucket{{ID: 10, Title: "Backlog", ProjectViewID: 1}}
	s := rt.summarize(task, false)
	if s.Status != "todo" || s.Bucket != "Backlog" {
		t.Fatalf("mapped bucket: status=%q bucket=%q", s.Status, s.Bucket)
	}
	var buf bytes.Buffer
	_ = rt.emitTask(&buf, task, false, "created")
	if !strings.Contains(buf.String(), " · Backlog · ") {
		t.Fatalf("line = %q", buf.String())
	}

	task.Buckets = []*client.Bucket{{ID: 99, Title: "Blocked", ProjectViewID: 1}, {ID: 5, Title: "Other view", ProjectViewID: 2}}
	s = rt.summarize(task, false)
	if s.Status != "" || s.Bucket != "Blocked" {
		t.Fatalf("unmapped bucket: status=%q bucket=%q", s.Status, s.Bucket)
	}
	buf.Reset()
	_ = rt.emitTask(&buf, task, false, "claimed")
	if !strings.Contains(buf.String(), " · Blocked · ") {
		t.Fatalf("line = %q", buf.String())
	}
}
