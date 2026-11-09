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
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"code.vikunja.io/veans/internal/client"
	"code.vikunja.io/veans/internal/config"
	"code.vikunja.io/veans/internal/output"
)

type fakeAttachServer struct {
	maxFileSize string
	uploadCode  int    // 0 → 201
	uploadBody  string // "" → echo the uploaded parts as success
	uploads     atomic.Int32
	gotNames    []string
	gotContents []string
}

func (f *fakeAttachServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/projects/1/tasks":
		_, _ = w.Write([]byte(`{"items":[{"id":224,"index":4,"project_id":1}],"total":1,"page":1,"per_page":50,"total_pages":1}`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/info":
		_, _ = w.Write([]byte(`{"version":"v2.7.1","max_file_size":"` + f.maxFileSize + `"}`))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v2/tasks/224/attachments":
		f.uploads.Add(1)
		if f.uploadCode != 0 {
			w.WriteHeader(f.uploadCode)
			_, _ = w.Write([]byte(f.uploadBody))
			return
		}
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var success []map[string]any
		for {
			p, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if p.FormName() != "files" {
				http.Error(w, "unexpected field "+p.FormName(), http.StatusBadRequest)
				return
			}
			b, _ := io.ReadAll(p)
			f.gotNames = append(f.gotNames, p.FileName())
			f.gotContents = append(f.gotContents, string(b))
			success = append(success, map[string]any{
				"id": 100 + len(success), "task_id": 224,
				"file": map[string]any{"id": 1, "name": p.FileName(), "mime": "text/plain", "size": len(b)},
			})
		}
		body := f.uploadBody
		if body == "" {
			buf, err := json.Marshal(map[string]any{"success": success, "errors": nil})
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			body = string(buf)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(body))
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}
}

func newAttachRuntime(t *testing.T, f *fakeAttachServer) *runtime {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &runtime{
		cfg:    &config.Config{Server: srv.URL, ProjectID: 1, ProjectIdentifier: "VKJ"},
		client: client.New(srv.URL, "tok"),
	}
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func attach(t *testing.T, rt *runtime, ref string, paths ...string) (string, error) {
	t.Helper()
	files, err := statAttachFiles(paths)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = runAttach(context.Background(), rt, &buf, ref, files)
	return buf.String(), err
}

func wantCode(t *testing.T, err error, code output.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s, got nil", code)
	}
	if got := output.AsError(err).Code; got != code {
		t.Fatalf("expected %s, got %s (%v)", code, got, err)
	}
}

func TestAttach_UploadsAllFilesInOneRequest(t *testing.T) {
	f := &fakeAttachServer{maxFileSize: "20MB"}
	rt := newAttachRuntime(t, f)
	a := writeTempFile(t, "a.txt", "hello")
	b := writeTempFile(t, "release.zip", "zipbytes")

	out, err := attach(t, rt, "VKJ-4", a, b)
	if err != nil {
		t.Fatal(err)
	}
	if f.uploads.Load() != 1 {
		t.Fatalf("expected one upload request, got %d", f.uploads.Load())
	}
	if strings.Join(f.gotNames, ",") != "a.txt,release.zip" || strings.Join(f.gotContents, ",") != "hello,zipbytes" {
		t.Fatalf("server got names=%v contents=%v", f.gotNames, f.gotContents)
	}
	var res attachResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if res.TaskID != 224 || len(res.Attachments) != 2 || len(res.Errors) != 0 {
		t.Fatalf("unexpected result %+v", res)
	}
	if res.Attachments[1].ID != 101 || res.Attachments[1].Name != "release.zip" || res.Attachments[1].Size != 8 {
		t.Fatalf("unexpected attachment %+v", res.Attachments[1])
	}
}

func TestAttach_TaskNotFound(t *testing.T) {
	f := &fakeAttachServer{maxFileSize: "20MB"}
	rt := newAttachRuntime(t, f)
	_, err := attach(t, rt, "VKJ-99", writeTempFile(t, "a.txt", "x"))
	wantCode(t, err, output.CodeNotFound)
	if f.uploads.Load() != 0 {
		t.Fatal("must not upload when the task does not resolve")
	}
}

func TestAttach_ServerNotFoundAndAuthMapToCodes(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   output.Code
	}{
		{http.StatusNotFound, output.CodeNotFound},
		{http.StatusForbidden, output.CodeAuth},
		{http.StatusUnauthorized, output.CodeAuth},
		{http.StatusInternalServerError, output.CodeUnknown},
	} {
		f := &fakeAttachServer{maxFileSize: "20MB", uploadCode: tc.status, uploadBody: `{"title":"nope","detail":"nope"}`}
		rt := newAttachRuntime(t, f)
		out, err := attach(t, rt, "#4", writeTempFile(t, "a.txt", "x"))
		wantCode(t, err, tc.code)
		if out != "" {
			t.Fatalf("status %d: stdout must stay empty on error, got %q", tc.status, out)
		}
	}
}

func TestAttach_MissingAndEmptyFiles(t *testing.T) {
	_, err := statAttachFiles([]string{filepath.Join(t.TempDir(), "nope.zip")})
	wantCode(t, err, output.CodeValidation)

	_, err = statAttachFiles([]string{writeTempFile(t, "empty.txt", "")})
	wantCode(t, err, output.CodeValidation)

	_, err = statAttachFiles([]string{t.TempDir()})
	wantCode(t, err, output.CodeValidation)
}

func TestAttach_RefusesFilesOverServerLimit(t *testing.T) {
	f := &fakeAttachServer{maxFileSize: "1KB"}
	rt := newAttachRuntime(t, f)
	small := writeTempFile(t, "small.txt", "x")
	big := writeTempFile(t, "big.bin", strings.Repeat("x", 1025))

	_, err := attach(t, rt, "VKJ-4", small, big)
	wantCode(t, err, output.CodeValidation)
	if !strings.Contains(err.Error(), "big.bin") || !strings.Contains(err.Error(), "1KB") {
		t.Fatalf("error should name the file and the limit: %v", err)
	}
	if f.uploads.Load() != 0 {
		t.Fatal("must not upload anything when a file is over the limit")
	}
}

func TestAttach_PartialFailurePrintsResultAndFails(t *testing.T) {
	f := &fakeAttachServer{
		maxFileSize: "20MB",
		uploadBody:  `{"success":[{"id":7,"task_id":224,"file":{"id":1,"name":"a.txt","mime":"text/plain","size":1}}],"errors":[{"code":4013,"message":"file b.txt is too large"}]}`,
	}
	rt := newAttachRuntime(t, f)
	out, err := attach(t, rt, "VKJ-4", writeTempFile(t, "a.txt", "x"), writeTempFile(t, "b.txt", "y"))
	wantCode(t, err, output.CodeValidation)
	var res attachResult
	if jerr := json.Unmarshal([]byte(out), &res); jerr != nil {
		t.Fatalf("stdout is not JSON: %v", jerr)
	}
	if len(res.Attachments) != 1 || res.Attachments[0].ID != 7 || len(res.Errors) != 1 {
		t.Fatalf("unexpected result %+v", res)
	}
}
