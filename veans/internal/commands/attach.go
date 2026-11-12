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
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"code.vikunja.io/veans/internal/client"
	"code.vikunja.io/veans/internal/output"
)

func newAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <id> <file> [<file>...]",
		Short: "Upload one or more files as attachments to a task",
		Long: `Uploads the given files as attachments to the task (PROJ-NN, #NN, or
numeric ID) in a single multipart request. Files must exist, be regular
files, be non-empty and fit the server's max_file_size; violations fail
with VALIDATION_ERROR before anything is uploaded.

Prints {"task_id","attachments":[{"id","name","size","mime"}],"errors":[...]}.
If the server rejects some files, the payload still lists the ones that
were attached and the command exits non-zero with VALIDATION_ERROR.

Example:
  veans attach PROJ-12 dist/release.zip`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := statAttachFiles(args[1:])
			if err != nil {
				return err
			}
			rt, err := loadRuntime(cmd.Context())
			if err != nil {
				return err
			}
			return runAttach(cmd.Context(), rt, cmd.OutOrStdout(), args[0], files)
		},
	}
}

type attachFile struct {
	path string
	size int64
}

type attachedFile struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size uint64 `json:"size"`
	Mime string `json:"mime"`
}

type attachResult struct {
	TaskID      int64                          `json:"task_id"`
	Attachments []attachedFile                 `json:"attachments"`
	Errors      []client.AttachmentUploadError `json:"errors"`
}

// statAttachFiles validates the local files before any network call so a
// typo fails fast and nothing is half-uploaded.
func statAttachFiles(paths []string) ([]attachFile, error) {
	out := make([]attachFile, 0, len(paths))
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return nil, output.Wrap(output.CodeValidation, err, "file %q: %v", p, err)
		}
		if !fi.Mode().IsRegular() {
			return nil, output.New(output.CodeValidation, "file %q is not a regular file", p)
		}
		if fi.Size() == 0 {
			return nil, output.New(output.CodeValidation, "file %q is empty", p)
		}
		out = append(out, attachFile{path: p, size: fi.Size()})
	}
	return out, nil
}

func runAttach(ctx context.Context, rt *runtime, w io.Writer, ref string, files []attachFile) error {
	id, err := rt.resolveTaskID(ctx, ref)
	if err != nil {
		return err
	}
	if err := checkUploadLimit(ctx, rt.client, files); err != nil {
		return err
	}

	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.path
	}
	res, err := rt.client.UploadTaskAttachments(ctx, id, paths)
	if err != nil {
		return err
	}

	out := attachResult{TaskID: id, Attachments: []attachedFile{}, Errors: res.Errors}
	if out.Errors == nil {
		out.Errors = []client.AttachmentUploadError{}
	}
	for _, a := range res.Success {
		af := attachedFile{ID: a.ID}
		if a.File != nil {
			af.Name, af.Size, af.Mime = a.File.Name, a.File.Size, a.File.Mime
		}
		out.Attachments = append(out.Attachments, af)
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		return err
	}
	if len(out.Errors) > 0 {
		msgs := make([]string, len(out.Errors))
		for i, e := range out.Errors {
			msgs[i] = e.Message
		}
		return output.New(output.CodeValidation, "%d of %d files were not attached: %s",
			len(out.Errors), len(files), strings.Join(msgs, "; "))
	}
	return nil
}

// checkUploadLimit refuses files larger than the server's max_file_size up
// front. When /info is unreachable or the value unparseable, the check is
// skipped: the server enforces the same limit per file anyway.
func checkUploadLimit(ctx context.Context, c *client.Client, files []attachFile) error {
	info, err := c.Info(ctx)
	if err != nil || info.MaxFileSize == "" {
		return nil //nolint:nilerr // best effort; the server still enforces the limit
	}
	limit, err := client.ParseFileSize(info.MaxFileSize)
	if err != nil || limit <= 0 {
		return nil //nolint:nilerr // best effort; the server still enforces the limit
	}
	for _, f := range files {
		if f.size > limit {
			return output.New(output.CodeValidation,
				"file %q is %d bytes, larger than the server's upload limit of %s",
				filepath.Base(f.path), f.size, info.MaxFileSize)
		}
	}
	return nil
}
