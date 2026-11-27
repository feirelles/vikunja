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

	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	var fields []string
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a task by PROJ-NN, #NN, or numeric ID",
		Long: `Show a task. The default output is a compact JSON object: id, identifier,
title, status, bucket, done, priority, label titles, assignee usernames, url,
description (HTML) and related task identifiers. --fields picks keys (from
that summary, then from the full task object); --json prints the full task.`,
		Example: `  veans show VKJ-6
  veans show VKJ-6 --fields title,status,url
  veans show VKJ-6 --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := loadRuntime(cmd.Context())
			if err != nil {
				return err
			}
			id, err := rt.resolveTaskID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			task, err := rt.client.GetTask(cmd.Context(), id)
			if err != nil {
				return err
			}
			rt.withURLs(task)
			enc := json.NewEncoder(cmd.OutOrStdout())
			switch {
			case wantJSON(cmd):
				return enc.Encode(task)
			case len(fields) > 0:
				m, err := rt.selectFields(task, fields)
				if err != nil {
					return err
				}
				return enc.Encode(m)
			}
			return enc.Encode(rt.summarize(task, true))
		},
	}
	cmd.Flags().StringSliceVar(&fields, "fields", nil, "comma-separated keys to print (e.g. title,status,description)")
	return cmd
}
