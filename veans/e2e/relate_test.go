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
	"strings"
	"testing"

	"code.vikunja.io/veans/internal/client"
)

// TestRelate_DerivedTaskInOneCommand covers VKJ-17 against a real server:
// create --related-to --md, relate --kind blocking, and the short output.
func TestRelate_DerivedTaskInOneCommand(t *testing.T) {
	ws, h := provisionWorkspace(t)

	out, _, code := h.Run(t, ws, "create", "origin", "--json")
	if code != 0 {
		t.Fatalf("create origin exit %d\n%s", code, out)
	}
	var origin client.Task
	if err := json.Unmarshal([]byte(out), &origin); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, ws)
	originRef := cfg.FormatTaskID(origin.Index)

	out, stderr, code := h.Run(t, ws, "create", "derived", "--label", "derived",
		"--related-to", originRef, "--md", "-d", "Found in **origin**.\n\n- [ ] check")
	if code != 0 {
		t.Fatalf("create derived exit %d\n%s\n%s", code, out, stderr)
	}
	if !strings.Contains(out, " created · Todo · ") || strings.Count(out, "\n") != 1 {
		t.Fatalf("short create output: %q", out)
	}
	derivedRef := strings.Fields(out)[0]

	shown, _, code := h.Run(t, ws, "show", derivedRef, "--json")
	if code != 0 {
		t.Fatalf("show exit %d", code)
	}
	var derived client.Task
	if err := json.Unmarshal([]byte(shown), &derived); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(derived.Description, `<li data-type="taskItem" data-checked="false"><p>check</p></li>`) ||
		!strings.Contains(derived.Description, "<strong>origin</strong>") {
		t.Fatalf("description not converted: %q", derived.Description)
	}
	if rel := derived.RelatedTasks["related"]; len(rel) != 1 || rel[0].ID != origin.ID {
		t.Fatalf("related tasks: %+v", derived.RelatedTasks)
	}

	out, _, code = h.Run(t, ws, "relate", derivedRef, originRef, "--kind", "blocking")
	if code != 0 || strings.TrimSpace(out) != fmt.Sprintf("%s blocking %s", derivedRef, originRef) {
		t.Fatalf("relate exit %d: %q", code, out)
	}
	out, _, code = h.Run(t, ws, "relate", derivedRef, originRef, "--kind", "blocking")
	if code != 0 || !strings.Contains(out, "(already)") {
		t.Fatalf("repeated relate exit %d: %q", code, out)
	}
	if blocked := h.GetTask(t, origin.ID).RelatedTasks["blocked"]; len(blocked) != 1 || blocked[0].ID != derived.ID {
		t.Fatalf("origin not blocked by derived: %+v", h.GetTask(t, origin.ID).RelatedTasks)
	}

	out, _, code = h.Run(t, ws, "update", derivedRef, "--md", "-c", "Note with `code`", "-s", "in-progress")
	if code != 0 || !strings.HasPrefix(out, derivedRef+" updated (status, comment) · In Progress · ") {
		t.Fatalf("update exit %d: %q", code, out)
	}
}
