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

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"code.vikunja.io/veans/internal/output"
)

// AttachmentFile mirrors the parent's files.File metadata embedded in an
// attachment.
type AttachmentFile struct {
	ID      int64     `json:"id"`
	Name    string    `json:"name"`
	Mime    string    `json:"mime"`
	Size    uint64    `json:"size"`
	Created time.Time `json:"created"`
}

// TaskAttachment mirrors the parent's models.TaskAttachment.
type TaskAttachment struct {
	ID      int64           `json:"id"`
	TaskID  int64           `json:"task_id"`
	File    *AttachmentFile `json:"file"`
	Created time.Time       `json:"created"`
}

// AttachmentUploadError is one per-file failure of an upload.
type AttachmentUploadError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// AttachmentUploadResult is the body of POST /tasks/{id}/attachments. Files
// are processed independently: a failed file lands in Errors while the rest
// still succeed, and the request returns 201 either way.
type AttachmentUploadResult struct {
	Success []*TaskAttachment       `json:"success"`
	Errors  []AttachmentUploadError `json:"errors"`
}

// UploadTaskAttachments sends the files at `paths` as one multipart request
// (one `files` part per path) to POST /api/v2/tasks/{id}/attachments.
//
// The body is streamed through a pipe so large files are not buffered in
// memory. The client timeout is lifted for this request: an upload of a
// server-sized file can legitimately outlast the default 30 s.
func (c *Client) UploadTaskAttachments(ctx context.Context, taskID int64, paths []string) (*AttachmentUploadResult, error) {
	path := fmt.Sprintf("/tasks/%d/attachments", taskID)
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		pw.CloseWithError(writeMultipartFiles(mw, paths))
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+apiBasePath+path, pr)
	if err != nil {
		_ = pr.Close()
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("User-Agent", UserAgent)

	hc := *c.HTTPClient
	hc.Timeout = 0
	resp, err := hc.Do(req)
	if err != nil {
		_ = pr.Close()
		return nil, output.Wrap(output.CodeUnknown, err, "POST %s: %v", path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, mapHTTPError("POST", path, resp.StatusCode, respBody,
			parseRetryAfter(resp.Header.Get("Retry-After")))
	}
	var out AttachmentUploadResult
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode POST %s: %w", path, err)
	}
	return &out, nil
}

func writeMultipartFiles(mw *multipart.Writer, paths []string) error {
	for _, p := range paths {
		if err := writeMultipartFile(mw, p); err != nil {
			return err
		}
	}
	return mw.Close()
}

func writeMultipartFile(mw *multipart.Writer, p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	part, err := mw.CreateFormFile("files", filepath.Base(p))
	if err != nil {
		return err
	}
	_, err = io.Copy(part, f)
	return err
}

// DeleteTaskAttachment removes one attachment from a task.
func (c *Client) DeleteTaskAttachment(ctx context.Context, taskID, attachmentID int64) error {
	return c.Do(ctx, "DELETE", fmt.Sprintf("/tasks/%d/attachments/%d", taskID, attachmentID), nil, nil, nil)
}

// ParseFileSize converts the server's `max_file_size` (e.g. "20MB", the
// c2h5oh/datasize text form) to bytes. Units are binary (1 KB = 1024 B), as
// the server computes them.
func ParseFileSize(s string) (int64, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	n, err := strconv.ParseInt(t[:i], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	var mult int64
	switch strings.TrimSpace(t[i:]) {
	case "", "b":
		mult = 1
	case "k", "kb":
		mult = 1 << 10
	case "m", "mb":
		mult = 1 << 20
	case "g", "gb":
		mult = 1 << 30
	case "t", "tb":
		mult = 1 << 40
	default:
		return 0, fmt.Errorf("invalid size unit in %q", s)
	}
	if n > (1<<62)/mult {
		return 1 << 62, nil
	}
	return n * mult, nil
}
