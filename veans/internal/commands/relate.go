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
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"code.vikunja.io/veans/internal/output"
)

// relationKinds are the relation kinds Vikunja accepts. A relation of kind k
// from A to B lists B under A's related_tasks[k].
var relationKinds = []string{
	"related", "subtask", "parenttask", "blocking", "blocked",
	"duplicateof", "duplicates", "precedes", "follows", "copiedfrom", "copiedto",
}

func validRelationKind(kind string) error {
	for _, k := range relationKinds {
		if kind == k {
			return nil
		}
	}
	return output.New(output.CodeValidation, "unknown relation kind %q (expected one of: %s)",
		kind, strings.Join(relationKinds, ", "))
}

func newRelateCmd() *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "relate <A> <B>",
		Short: "Relate two tasks: B becomes A's <kind> (default kind: related)",
		Long: `Add B to A's related tasks under <kind>: --kind subtask makes B a subtask
of A, --kind blocking means A blocks B. Vikunja records the inverse on B.

A is a task of this project (PROJ-NN, #NN or NN). B may also be a task URL
(https://…/tasks/<id>), which reaches tasks in other projects. Relating two
tasks that are already related the same way succeeds without a change.`,
		Example: `  veans relate VKJ-17 VKJ-16
  veans relate VKJ-18 VKJ-22 --kind subtask
  veans relate VKJ-16 https://tasks.example.com/tasks/540 --kind blocking`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validRelationKind(kind); err != nil {
				return err
			}
			rt, err := loadRuntime(cmd.Context())
			if err != nil {
				return err
			}
			a, err := rt.resolveTaskID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			b, err := rt.resolveRelationTarget(cmd.Context(), args[1])
			if err != nil {
				return err
			}
			rel, existed, err := rt.relate(cmd.Context(), a, b, kind)
			if err != nil {
				return err
			}
			if wantJSON(cmd) {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(rel)
			}
			line := fmt.Sprintf("%s %s %s", args[0], kind, args[1])
			if existed {
				line += " (already)"
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), line)
			return err
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "related", "relation kind: "+strings.Join(relationKinds, "|"))
	return cmd
}

// relationResult is what `relate --json` prints.
type relationResult struct {
	TaskID       int64  `json:"task_id"`
	OtherTaskID  int64  `json:"other_task_id"`
	RelationKind string `json:"relation_kind"`
	Existed      bool   `json:"existed"`
}

// relate creates "a kind b". A CONFLICT from Vikunja (the relation exists)
// counts as success, so re-running a command is safe.
func (r *runtime) relate(ctx context.Context, a, b int64, kind string) (*relationResult, bool, error) {
	if a == b {
		return nil, false, output.New(output.CodeValidation, "a task cannot be related to itself")
	}
	res := &relationResult{TaskID: a, OtherTaskID: b, RelationKind: kind}
	if _, err := r.client.CreateRelation(ctx, a, b, kind); err != nil {
		var oe *output.Error
		if !errors.As(err, &oe) || oe.Code != output.CodeConflict {
			return nil, false, err
		}
		res.Existed = true
	}
	return res, res.Existed, nil
}

// resolveRelationTarget accepts everything resolveTaskID does plus a task URL
// (…/tasks/<id>), whose numeric id is used as is.
func (r *runtime) resolveRelationTarget(ctx context.Context, raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		u, err := url.Parse(raw)
		if err == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if n := len(parts); n >= 2 && parts[n-2] == "tasks" {
				if id, err := strconv.ParseInt(parts[n-1], 10, 64); err == nil && id > 0 {
					return id, nil
				}
			}
		}
		return 0, output.New(output.CodeValidation, "invalid task URL %q (expected …/tasks/<id>)", raw)
	}
	return r.resolveTaskID(ctx, raw)
}
