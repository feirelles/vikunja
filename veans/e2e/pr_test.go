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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code.vikunja.io/veans/internal/client"
)

// fakeGHScript answers `gh pr list` with no PRs and `gh pr create` with a
// URL, logging every call, so `veans pr` runs end to end without GitHub.
const fakeGHScript = `#!/bin/sh
echo "$@" >> "$FAKE_GH_LOG"
case "$1 $2" in
  "pr list") echo "[]" ;;
  "pr create") cat > "$FAKE_GH_LOG.body"; echo "https://github.com/example/repo/pull/77" ;;
  *) echo "unexpected gh $*" >&2; exit 1 ;;
esac
`

// TestPR_OneCommandOpensPRAndMovesToInReview is the VKJ-18 acceptance: one
// command leaves the PR open and the task in In Review with link and summary.
func TestPR_OneCommandOpensPRAndMovesToInReview(t *testing.T) {
	ws, h := provisionWorkspace(t)

	out, _, code := h.Run(t, ws, "create", "pr me", "--json")
	if code != 0 {
		t.Fatalf("create exit %d\n%s", code, out)
	}
	var created client.Task
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("%d", created.Index)

	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGHScript), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "gh.log")
	ws.envOverrides["PATH"] = bin + string(os.PathListSeparator) + os.Getenv("PATH")
	ws.envOverrides["FAKE_GH_LOG"] = log

	stdout, stderr, code := h.Run(t, ws, "pr", id,
		"--head", "feat-pr-test", "--title", "feat: pr test", "--body", "Body.",
		"--summary", "Changed **x**.\n\n- tests ok")
	if code != 0 {
		t.Fatalf("pr exit %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "PR https://github.com/example/repo/pull/77") || !strings.Contains(stdout, "In Review") {
		t.Fatalf("pr output: %q", stdout)
	}

	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "pr create --base main --head feat-pr-test") || strings.Contains(string(calls), "merge") {
		t.Fatalf("gh calls:\n%s", calls)
	}
	body, _ := os.ReadFile(log + ".body")
	if !strings.HasSuffix(string(body), "Refs: "+loadConfig(t, ws).FormatTaskID(created.Index)+"\n") {
		t.Fatalf("PR body: %q", body)
	}

	cfg := loadConfig(t, ws)
	server := h.GetTask(t, created.ID)
	if got := server.CurrentBucketID(cfg.ViewID); got != cfg.Buckets.InReview {
		t.Fatalf("task in bucket %d, want In Review %d", got, cfg.Buckets.InReview)
	}
	comments, err := h.AdminClient.ListTaskComments(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || !strings.Contains(comments[0].Comment, "pull/77") ||
		!strings.Contains(comments[0].Comment, "<strong>x</strong>") {
		t.Fatalf("comments: %+v", comments)
	}
}
