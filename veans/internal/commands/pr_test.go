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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"code.vikunja.io/veans/internal/client"
	"code.vikunja.io/veans/internal/config"
)

// fakePRServer is the slice of Vikunja `veans pr` touches. Task VKJ-18 is
// id 142; the bucket move and every write are recorded.
type fakePRServer struct {
	t        *testing.T
	mu       sync.Mutex
	writes   []string
	comments []string
	bucket   int64
}

func (f *fakePRServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method != http.MethodGet {
		f.writes = append(f.writes, r.Method+" "+r.URL.Path)
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/projects/7/tasks":
		_, _ = w.Write([]byte(`{"items":[{"id":142,"index":18,"project_id":7}],"total":1,"page":1,"per_page":50,"total_pages":1}`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/tasks/142":
		bucket := f.bucket
		if bucket == 0 {
			bucket = 11
		}
		fmt.Fprintf(w, `{"id":142,"index":18,"project_id":7,"title":"veans pr","updated":"2026-01-01T00:00:00Z","buckets":[{"id":%d,"project_view_id":1}]}`, bucket)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/info":
		_, _ = w.Write([]byte(`{"max_file_size":"20MB"}`))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v2/tasks/142/attachments":
		_, _ = w.Write([]byte(`{"success":[{"id":9,"task_id":142,"file":{"id":1,"name":"shot.png","mime":"image/png","size":4}}],"errors":null}`))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v2/tasks/142/comments":
		var body client.TaskComment
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.comments = append(f.comments, body.Comment)
		_, _ = w.Write([]byte(`{"id":1}`))
	case r.Method == http.MethodPatch && r.URL.Path == "/api/v2/tasks/142":
		_, _ = w.Write([]byte(`{"id":142,"index":18,"project_id":7}`))
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v2/projects/7/views/1/buckets/"):
		_, _ = fmt.Sscanf(r.URL.Path, "/api/v2/projects/7/views/1/buckets/%d/tasks", &f.bucket)
		_, _ = w.Write([]byte(`{"task_id":142}`))
	default:
		f.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}
}

// fakeGH replaces ghRunner. list is what `gh pr list` prints; createErr
// and listErr make those calls fail.
type fakeGH struct {
	list      string
	listErr   error
	createOut string
	createErr error
	calls     [][]string
	stdin     []string
}

func (g *fakeGH) run(_ context.Context, stdin string, args ...string) (string, error) {
	g.calls = append(g.calls, args)
	g.stdin = append(g.stdin, stdin)
	switch {
	case len(args) >= 2 && args[0] == "pr" && args[1] == "list":
		return g.list, g.listErr
	case len(args) >= 2 && args[0] == "pr" && args[1] == "create":
		return g.createOut, g.createErr
	}
	return "", fmt.Errorf("unexpected gh call %v", args)
}

func (g *fakeGH) calledWith(sub string) []string {
	for _, c := range g.calls {
		if len(c) >= 2 && c[1] == sub {
			return c
		}
	}
	return nil
}

func setupPR(t *testing.T, gh *fakeGH) (*fakePRServer, *runtime) {
	t.Helper()
	f := &fakePRServer{t: t}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	oldRunner, oldGap := ghRunner, writeGap
	ghRunner, writeGap = gh.run, 0
	t.Cleanup(func() { ghRunner, writeGap = oldRunner, oldGap })

	rt := &runtime{
		cfg: &config.Config{
			Server: srv.URL, ProjectID: 7, ProjectIdentifier: "VKJ", ViewID: 1,
			Buckets: config.Buckets{Todo: 10, InProgress: 11, InReview: 12, Done: 13, Scrapped: 14},
			PRBase:  "release/v2.7.1-fork", PRRepo: "feirelles/vikunja",
		},
		client: client.New(srv.URL, "tk"),
	}
	return f, rt
}

func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func assertNeverMerges(t *testing.T, gh *fakeGH) {
	t.Helper()
	for _, c := range gh.calls {
		for _, a := range c {
			if a == "merge" {
				t.Fatalf("gh merge called: %v", c)
			}
		}
	}
}

func TestPR_NewPR(t *testing.T) {
	gh := &fakeGH{list: "[]\n", createOut: "Creating pull request…\nhttps://github.com/feirelles/vikunja/pull/20\n"}
	f, rt := setupPR(t, gh)

	res, err := runPR(context.Background(), rt, "VKJ-18", &prFlags{
		title: "feat(veans): add pr", body: "Body text.", head: "feat/veans-pr",
		summary: "Adds `veans pr`.\n\n- tests ok",
	})
	if err != nil {
		t.Fatalf("runPR: %v", err)
	}
	create := gh.calledWith("create")
	if create == nil {
		t.Fatalf("gh pr create not called: %v", gh.calls)
	}
	if argValue(create, "--base") != "release/v2.7.1-fork" || argValue(create, "--head") != "feat/veans-pr" ||
		argValue(create, "--repo") != "feirelles/vikunja" || argValue(create, "--title") != "feat(veans): add pr" ||
		argValue(create, "--body-file") != "-" {
		t.Fatalf("gh pr create args: %v", create)
	}
	if body := gh.stdin[len(gh.stdin)-1]; body != "Body text.\n\nRefs: VKJ-18\n" {
		t.Fatalf("PR body = %q", body)
	}
	if list := gh.calledWith("list"); argValue(list, "--head") != "feat/veans-pr" || argValue(list, "--repo") != "feirelles/vikunja" {
		t.Fatalf("gh pr list args: %v", list)
	}
	if res.Existing || res.PRURL != "https://github.com/feirelles/vikunja/pull/20" {
		t.Fatalf("result: %+v", res)
	}
	if f.bucket != 12 {
		t.Fatalf("task moved to bucket %d, want In Review (12)", f.bucket)
	}
	if len(f.comments) != 1 {
		t.Fatalf("comments: %v", f.comments)
	}
	want := `<p><strong>PR:</strong> <a href="https://github.com/feirelles/vikunja/pull/20">https://github.com/feirelles/vikunja/pull/20</a></p>` +
		"\n<p>Adds <code>veans pr</code>.</p>\n<ul>\n<li><p>tests ok</p></li>\n</ul>"
	if f.comments[0] != want {
		t.Fatalf("comment:\nwant %q\ngot  %q", want, f.comments[0])
	}
	if res.Task == nil || res.Task.CurrentBucketID(1) != 12 {
		t.Fatalf("returned task not In Review: %+v", res.Task)
	}
	assertNeverMerges(t, gh)
}

func TestPR_ExistingPROnlyUpdatesTask(t *testing.T) {
	gh := &fakeGH{list: `[{"url":"https://github.com/feirelles/vikunja/pull/5"}]`}
	f, rt := setupPR(t, gh)

	res, err := runPR(context.Background(), rt, "VKJ-18", &prFlags{head: "feat/veans-pr"})
	if err != nil {
		t.Fatalf("runPR: %v", err)
	}
	if gh.calledWith("create") != nil {
		t.Fatalf("gh pr create called for a branch with an open PR: %v", gh.calls)
	}
	if !res.Existing || res.PRURL != "https://github.com/feirelles/vikunja/pull/5" {
		t.Fatalf("result: %+v", res)
	}
	if f.bucket != 12 || len(f.comments) != 1 || !strings.Contains(f.comments[0], "pull/5") {
		t.Fatalf("task not updated: bucket=%d comments=%v", f.bucket, f.comments)
	}
	assertNeverMerges(t, gh)
}

// If gh fails, the task must not change.
func TestPR_GHFailureLeavesTaskAlone(t *testing.T) {
	cases := map[string]*fakeGH{
		"create fails": {list: "[]", createErr: errors.New("gh pr create: no commits between base and head")},
		"list fails":   {listErr: errors.New("gh pr list: not logged in")},
		"no URL":       {list: "[]", createOut: "something odd\n"},
	}
	for name, gh := range cases {
		t.Run(name, func(t *testing.T) {
			f, rt := setupPR(t, gh)
			_, err := runPR(context.Background(), rt, "VKJ-18", &prFlags{title: "t", head: "feat/veans-pr"})
			if err == nil {
				t.Fatal("want error")
			}
			if len(f.writes) != 0 {
				t.Fatalf("Vikunja writes after gh failure: %v", f.writes)
			}
		})
	}
}

func TestPR_AttachBeforeComment(t *testing.T) {
	gh := &fakeGH{list: "[]", createOut: "https://github.com/feirelles/vikunja/pull/21\n"}
	f, rt := setupPR(t, gh)
	shot := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(shot, []byte("png!"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := runPR(context.Background(), rt, "VKJ-18", &prFlags{title: "t", head: "feat/veans-pr", attach: []string{shot}})
	if err != nil {
		t.Fatalf("runPR: %v", err)
	}
	if len(f.writes) == 0 || f.writes[0] != "POST /api/v2/tasks/142/attachments" {
		t.Fatalf("attachment must be the first write: %v", f.writes)
	}
	if len(res.Attachments) != 1 || !strings.Contains(f.comments[0], "<code>shot.png</code>") {
		t.Fatalf("attachment not reported: %+v %v", res.Attachments, f.comments)
	}
}

// Local mistakes fail before gh or Vikunja are touched.
func TestPR_ValidationBeforeAnything(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.md")
	cases := map[string]*prFlags{
		"no branch":           {title: "t"},
		"head is base":        {title: "t", head: "release/v2.7.1-fork"},
		"missing body file":   {title: "t", head: "feat/x", bodyFile: missing},
		"missing attachment":  {title: "t", head: "feat/x", attach: []string{missing}},
		"body and body-file":  {title: "t", head: "feat/x", body: "a", bodyFile: missing},
		"summary and file":    {title: "t", head: "feat/x", summary: "a", summaryFile: missing},
		"missing summaryfile": {title: "t", head: "feat/x", summaryFile: missing},
	}
	for name, fl := range cases {
		t.Run(name, func(t *testing.T) {
			gh := &fakeGH{list: "[]", createOut: "https://github.com/o/r/pull/1\n"}
			f, rt := setupPR(t, gh)
			_, err := runPR(context.Background(), rt, "VKJ-18", fl)
			wantCode(t, err, "VALIDATION_ERROR")
			if len(gh.calls) != 0 || len(f.writes) != 0 {
				t.Fatalf("side effects: gh=%v writes=%v", gh.calls, f.writes)
			}
		})
	}
}

// Without an open PR a title is required; gh create is never called.
func TestPR_TitleRequiredForNewPR(t *testing.T) {
	gh := &fakeGH{list: "[]"}
	f, rt := setupPR(t, gh)
	_, err := runPR(context.Background(), rt, "VKJ-18", &prFlags{head: "feat/x"})
	wantCode(t, err, "VALIDATION_ERROR")
	if gh.calledWith("create") != nil || len(f.writes) != 0 {
		t.Fatalf("side effects: gh=%v writes=%v", gh.calls, f.writes)
	}
}

func TestPR_DefaultBaseAndDraft(t *testing.T) {
	gh := &fakeGH{list: "[]", createOut: "https://github.com/o/r/pull/2\n"}
	_, rt := setupPR(t, gh)
	rt.cfg.PRBase, rt.cfg.PRRepo = "", ""
	if _, err := runPR(context.Background(), rt, "VKJ-18", &prFlags{title: "t", head: "feat/x", draft: true}); err != nil {
		t.Fatal(err)
	}
	create := gh.calledWith("create")
	if argValue(create, "--base") != "main" || argValue(create, "--repo") != "" || create[len(create)-1] != "--draft" {
		t.Fatalf("gh pr create args: %v", create)
	}
}

func TestWithRefs(t *testing.T) {
	cases := map[string]string{
		"":                       "Refs: VKJ-18\n",
		"Body":                   "Body\n\nRefs: VKJ-18\n",
		"Body\n\n":               "Body\n\nRefs: VKJ-18\n",
		"Body\n\nRefs: VKJ-18\n": "Body\n\nRefs: VKJ-18\n",
		"Body\nrefs: vkj-18":     "Body\nrefs: vkj-18",
		"Body\n\nRefs: VKJ-17\n": "Body\n\nRefs: VKJ-17\n\nRefs: VKJ-18\n",
	}
	for in, want := range cases {
		if got := withRefs(in, "VKJ-18"); got != want {
			t.Errorf("withRefs(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckoutBranch(t *testing.T) {
	type call = string
	fake := func(existing bool, failOn string, calls *[]call) func(context.Context, ...string) (string, error) {
		return func(_ context.Context, args ...string) (string, error) {
			c := strings.Join(args, " ")
			*calls = append(*calls, c)
			if strings.HasPrefix(c, "rev-parse") && !existing {
				return "", errors.New("exit 1")
			}
			if failOn != "" && strings.HasPrefix(c, failOn) {
				return "", errors.New("boom")
			}
			return "", nil
		}
	}

	var calls []call
	if err := checkoutBranch(context.Background(), fake(false, "", &calls), "feat/x", "release/r"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, "|") != "rev-parse --verify --quiet refs/heads/feat/x|fetch origin release/r|switch --no-track -c feat/x origin/release/r" {
		t.Fatalf("new branch calls: %v", calls)
	}

	calls = nil
	if err := checkoutBranch(context.Background(), fake(true, "", &calls), "feat/x", "main"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, "|") != "rev-parse --verify --quiet refs/heads/feat/x|switch feat/x" {
		t.Fatalf("existing branch calls: %v", calls)
	}

	calls = nil
	if err := checkoutBranch(context.Background(), fake(false, "fetch", &calls), "feat/x", "main"); err == nil {
		t.Fatal("fetch failure must fail")
	}
}

func TestClaimCmd_CheckoutNeedsBranch(t *testing.T) {
	cmd := newClaimCmd()
	cmd.SetArgs([]string{"VKJ-6", "--checkout"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	wantCode(t, cmd.Execute(), "VALIDATION_ERROR")
}

func TestConfig_PRBaseBranch(t *testing.T) {
	if got := (&config.Config{}).PRBaseBranch(); got != "main" {
		t.Fatalf("default = %q", got)
	}
	if got := (&config.Config{PRBase: " dev "}).PRBaseBranch(); got != "dev" {
		t.Fatalf("pr_base = %q", got)
	}
}
