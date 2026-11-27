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
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"code.vikunja.io/veans/internal/client"
	"code.vikunja.io/veans/internal/output"
	"code.vikunja.io/veans/internal/status"
)

// jsonFlag is the persistent root flag that restores the full task objects
// on stdout. Without it, write commands print one line and show/list print
// the compact summary below: agents pay for every byte of output in context.
const jsonFlag = "json"

func wantJSON(cmd *cobra.Command) bool {
	v, err := cmd.Flags().GetBool(jsonFlag)
	return err == nil && v
}

// taskSummary is the compact shape `show` and `list` emit by default.
type taskSummary struct {
	ID          int64               `json:"id"`
	Identifier  string              `json:"identifier"`
	Title       string              `json:"title"`
	Status      string              `json:"status,omitempty"`
	Done        bool                `json:"done"`
	Priority    int64               `json:"priority,omitempty"`
	Labels      []string            `json:"labels"`
	Assignees   []string            `json:"assignees,omitempty"`
	URL         string              `json:"url,omitempty"`
	Description string              `json:"description,omitempty"`
	Related     map[string][]string `json:"related,omitempty"`
}

// taskStatus is the canonical status of t's bucket on the configured view, or
// the bucket's own title when it maps to none (a project may add buckets such
// as Backlog or Blocked next to the five veans knows).
func (r *runtime) taskStatus(t *client.Task) string {
	if st := status.FromBucketID(t.CurrentBucketID(r.cfg.ViewID), r.cfg.Buckets); st != "" {
		return string(st)
	}
	return r.viewBucket(t)
}

func (r *runtime) bucketTitle(t *client.Task) string {
	if st := status.FromBucketID(t.CurrentBucketID(r.cfg.ViewID), r.cfg.Buckets); st != "" {
		return st.BucketTitle()
	}
	return r.viewBucket(t)
}

func (r *runtime) viewBucket(t *client.Task) string {
	for _, b := range t.Buckets {
		if b != nil && b.ProjectViewID == r.cfg.ViewID {
			return b.Title
		}
	}
	return ""
}

func (r *runtime) summarize(t *client.Task, withBody bool) *taskSummary {
	s := &taskSummary{
		ID:         t.ID,
		Identifier: r.cfg.FormatTaskID(t.Index),
		Title:      t.Title,
		Status:     r.taskStatus(t),
		Done:       t.Done,
		Priority:   t.Priority,
		Labels:     []string{},
		URL:        t.URL,
	}
	for _, l := range t.Labels {
		if l != nil {
			s.Labels = append(s.Labels, l.Title)
		}
	}
	for _, a := range t.Assignees {
		if a != nil {
			s.Assignees = append(s.Assignees, a.Username)
		}
	}
	if withBody {
		s.Description = t.Description
		for kind, tasks := range t.RelatedTasks {
			for _, rt := range tasks {
				if rt == nil {
					continue
				}
				if s.Related == nil {
					s.Related = map[string][]string{}
				}
				ref := rt.Identifier
				switch {
				case rt.ProjectID == r.cfg.ProjectID && rt.Index != 0:
					ref = r.cfg.FormatTaskID(rt.Index)
				case ref == "":
					ref = fmt.Sprintf("#%d", rt.ID)
				}
				s.Related[kind] = append(s.Related[kind], ref)
			}
		}
	}
	return s
}

// selectFields narrows a task to the given JSON keys. Keys come from the
// compact summary first (so `labels` stays a list of titles and `status`
// exists) and fall back to the full task object for everything else.
func (r *runtime) selectFields(t *client.Task, fields []string) (map[string]any, error) {
	full, err := toMap(t)
	if err != nil {
		return nil, err
	}
	summary, err := toMap(r.summarize(t, true))
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(fields))
	for _, raw := range fields {
		f := strings.TrimSpace(raw)
		if f == "" {
			continue
		}
		if v, ok := summary[f]; ok {
			out[f] = v
			continue
		}
		if v, ok := full[f]; ok {
			out[f] = v
			continue
		}
		if f == "status" || f == "related" || f == "assignees" || f == "description" || f == "priority" {
			out[f] = nil // known key, empty on this task
			continue
		}
		return nil, output.New(output.CodeValidation, "unknown field %q", f)
	}
	return out, nil
}

func toMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	return m, json.Unmarshal(b, &m)
}

// writeTaskLine prints the one-line result of a write command:
//
//	VKJ-16 claimed · In Progress · https://…/tasks/601
//
// verb may carry a parenthesised list of what changed.
func (r *runtime) writeTaskLine(w io.Writer, t *client.Task, verb string) error {
	parts := []string{r.cfg.FormatTaskID(t.Index) + " " + verb}
	if title := r.bucketTitle(t); title != "" {
		parts = append(parts, title)
	}
	if t.URL != "" {
		parts = append(parts, t.URL)
	}
	_, err := fmt.Fprintln(w, strings.Join(parts, " · "))
	return err
}

// emitTask is the shared tail of create/update/claim: the full object with
// --json, one line otherwise.
func (r *runtime) emitTask(w io.Writer, t *client.Task, asJSON bool, verb string) error {
	r.withURLs(t)
	if asJSON {
		return json.NewEncoder(w).Encode(t)
	}
	return r.writeTaskLine(w, t, verb)
}
