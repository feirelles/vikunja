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
	"io"
	"strings"

	"github.com/spf13/cobra"

	"code.vikunja.io/veans/internal/client"
	"code.vikunja.io/veans/internal/output"
	"code.vikunja.io/veans/internal/status"
)

func newClaimCmd() *cobra.Command {
	var branchFlag string
	cmd := &cobra.Command{
		Use:   "claim <id>",
		Short: "Claim a task: assign the bot, move to In Progress, tag with branch",
		Long: `Claim a task: assign the bot, move it to In Progress and tag it with a
veans:branch:<name> label.

Without --branch the label uses the branch of the current git checkout (none
when HEAD is detached or outside a repo). With --branch <name> the label uses
<name> instead, and any other veans:branch:* label already on the task is
removed, so re-running claim with the right branch fixes a wrong label.`,
		Example: `  veans claim VKJ-6
  veans claim VKJ-6 --branch feat/veans-claim-branch`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicit := cmd.Flags().Changed("branch")
			branch := strings.TrimSpace(branchFlag)
			if explicit && branch == "" {
				return output.New(output.CodeValidation, "--branch must not be empty")
			}
			rt, err := loadRuntime(cmd.Context())
			if err != nil {
				return err
			}
			if !explicit {
				branch = currentGitBranch(cmd.Context())
			}
			return runClaim(cmd.Context(), rt, cmd.OutOrStdout(), args[0], branch, explicit)
		},
	}
	cmd.Flags().StringVar(&branchFlag, "branch", "",
		"branch name for the veans:branch:<name> label instead of the current checkout's; replaces other veans:branch:* labels on the task")
	return cmd
}

// runClaim does the work of `veans claim`. branch is the name for the
// veans:branch:<name> label ("" skips labelling). replaceBranchLabels is set
// when the branch came from --branch: other veans:branch:* labels on the task
// are then detached after the new one is attached. Without the flag the
// existing labels are left alone, as before.
func runClaim(ctx context.Context, rt *runtime, w io.Writer, ref, branch string, replaceBranchLabels bool) error {
	id, err := rt.resolveTaskID(ctx, ref)
	if err != nil {
		return err
	}

	// Move to In Progress. Vikunja's task↔bucket relation lives
	// in a separate table; POST /tasks doesn't move buckets, so
	// use the dedicated endpoint.
	bid, err := status.BucketID(status.InProgress, rt.cfg.Buckets)
	if err != nil {
		return err
	}
	if err := rt.client.MoveTaskToBucket(ctx,
		rt.cfg.ProjectID, rt.cfg.ViewID, bid, id); err != nil {
		return err
	}

	current, err := rt.client.GetTask(ctx, id)
	if err != nil {
		return err
	}

	// Assign the bot unless it already is: Vikunja answers a repeated
	// assign with 400 ("already assigned"), which would abort a re-claim
	// before the branch label is fixed. A 409 is still a soft-skip.
	if !hasAssignee(current, rt.cfg.Bot.UserID) {
		if err := rt.client.AddAssignee(ctx, id, rt.cfg.Bot.UserID); err != nil {
			var oe *output.Error
			if !errors.As(err, &oe) || oe.Code != output.CodeConflict {
				return err
			}
		}
	}

	if branch != "" {
		if err := tagClaimBranch(ctx, rt, current, branchLabel(branch), replaceBranchLabels); err != nil {
			return err
		}
	}

	task, err := rt.client.GetTask(ctx, id)
	if err != nil {
		return err
	}
	rt.withURLs(task)
	return json.NewEncoder(w).Encode(task)
}

func hasAssignee(t *client.Task, userID int64) bool {
	for _, a := range t.Assignees {
		if a != nil && a.ID == userID {
			return true
		}
	}
	return false
}

// tagClaimBranch attaches the label titled labelTitle to the task. With
// replace set, every other veans:branch:* label on the task is detached
// afterwards. Attaching first means a failed attach never leaves the task
// without any branch label.
func tagClaimBranch(ctx context.Context, rt *runtime, current *client.Task, labelTitle string, replace bool) error {
	taskID := current.ID
	var stale []int64
	present := false
	if replace {
		for _, l := range current.Labels {
			if l == nil || !strings.HasPrefix(l.Title, branchLabel("")) {
				continue
			}
			if l.Title == labelTitle {
				present = true
				continue
			}
			stale = append(stale, l.ID)
		}
	}

	if !present {
		l, err := getOrCreateLabelByTitle(ctx, rt.client, labelTitle)
		if err != nil {
			return err
		}
		if err := rt.client.AddLabelToTask(ctx, taskID, l.ID); err != nil {
			var oe *output.Error
			if !errors.As(err, &oe) || oe.Code != output.CodeConflict {
				return err
			}
		}
	}

	for _, labelID := range stale {
		if err := rt.client.RemoveLabelFromTask(ctx, taskID, labelID); err != nil {
			var oe *output.Error
			if !errors.As(err, &oe) || oe.Code != output.CodeNotFound {
				return err
			}
		}
	}
	return nil
}
