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
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"code.vikunja.io/veans/internal/client"
	"code.vikunja.io/veans/internal/output"
)

// ghRunner runs `gh <args...>` with stdin and returns its stdout. Tests
// replace it; nothing else may call gh, so `veans pr` can never merge.
var ghRunner = func(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("gh %s: %s", args[0]+" "+args[1], msg)
	}
	return stdout.String(), nil
}

// writeGap spaces the separate Vikunja write stages of `veans pr`: on SQLite
// back-to-back writes from several sessions can hit "database is locked".
var writeGap = 1500 * time.Millisecond

type prFlags struct {
	title       string
	body        string
	bodyFile    string
	summary     string
	summaryFile string
	attach      []string
	base        string
	head        string
	repo        string
	draft       bool
}

type prResult struct {
	PRURL       string         `json:"pr_url"`
	Existing    bool           `json:"existing"`
	Base        string         `json:"base"`
	Head        string         `json:"head"`
	Attachments []attachedFile `json:"attachments"`
	Task        *client.Task   `json:"task"`
}

func newPRCmd() *cobra.Command {
	f := &prFlags{}
	cmd := &cobra.Command{
		Use:   "pr <id>",
		Short: "Open the pull request for a task, link it on the task and move it to In Review",
		Long: `Open a GitHub pull request for the current branch with gh, then comment the
PR link and the summary on the task and move the task to In Review.

The PR goes against --base, else pr_base in .veans.yml, else "main"; the
repository is --repo, else pr_repo, else gh's default. A "Refs: <ID>" line
is appended to the body unless it is already there.

If the branch already has an open PR, no PR is created: the task is
updated with that PR's link. If gh fails, the task is not touched.

--summary (or --summary-file) is the review summary posted with the link:
what changed, test evidence, how to check. It is markdown (HTML passes
through). --attach uploads files (screenshots) to the task first.

veans pr never merges and never moves a task to Done.`,
		Example: `  veans pr VKJ-18 --title "feat(veans): add pr command" --body-file /tmp/body.md \
    --summary 'Adds veans pr. Tests: go test ./... ok.' --attach shot.png`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := loadRuntime(cmd.Context())
			if err != nil {
				return err
			}
			if f.head == "" {
				f.head = currentGitBranch(cmd.Context())
			}
			res, err := runPR(cmd.Context(), rt, args[0], f)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if wantJSON(cmd) {
				return json.NewEncoder(w).Encode(res)
			}
			verb := "PR " + res.PRURL
			if res.Existing {
				verb += " (existing)"
			}
			return rt.writeTaskLine(w, res.Task, verb)
		},
	}
	cmd.Flags().StringVar(&f.title, "title", "", "PR title (required unless the branch already has an open PR)")
	cmd.Flags().StringVar(&f.body, "body", "", "PR body")
	cmd.Flags().StringVar(&f.bodyFile, "body-file", "", "read the PR body from a file")
	cmd.Flags().StringVar(&f.summary, "summary", "", "review summary for the task comment (markdown)")
	cmd.Flags().StringVar(&f.summaryFile, "summary-file", "", "read the review summary from a file")
	cmd.Flags().StringSliceVar(&f.attach, "attach", nil, "file to attach to the task (repeatable)")
	cmd.Flags().StringVar(&f.base, "base", "", "base branch (default: pr_base from .veans.yml, else main)")
	cmd.Flags().StringVar(&f.head, "head", "", "head branch (default: current branch)")
	cmd.Flags().StringVar(&f.repo, "repo", "", "GitHub repository OWNER/REPO (default: pr_repo from .veans.yml, else gh's default)")
	cmd.Flags().BoolVar(&f.draft, "draft", false, "open the PR as a draft")
	return cmd
}

// runPR validates every local input, resolves the task, finds or opens the
// PR, and only then writes to Vikunja: attachments, then one update that
// posts the comment and moves the task to In Review.
func runPR(ctx context.Context, rt *runtime, ref string, f *prFlags) (*prResult, error) {
	head := strings.TrimSpace(f.head)
	if head == "" {
		return nil, output.New(output.CodeValidation, "no branch: check out the task's branch or pass --head")
	}
	base := strings.TrimSpace(f.base)
	if base == "" {
		base = rt.cfg.PRBaseBranch()
	}
	if head == base {
		return nil, output.New(output.CodeValidation, "head and base are both %q: open the PR from a feature branch", head)
	}
	repo := strings.TrimSpace(f.repo)
	if repo == "" {
		repo = strings.TrimSpace(rt.cfg.PRRepo)
	}
	if f.body != "" && f.bodyFile != "" {
		return nil, output.New(output.CodeValidation, "--body and --body-file are mutually exclusive")
	}
	if f.summary != "" && f.summaryFile != "" {
		return nil, output.New(output.CodeValidation, "--summary and --summary-file are mutually exclusive")
	}
	body, err := readTextFlag(f.body, f.bodyFile, "--body-file")
	if err != nil {
		return nil, err
	}
	summary, err := readTextFlag(f.summary, f.summaryFile, "--summary-file")
	if err != nil {
		return nil, err
	}
	files, err := statAttachFiles(f.attach)
	if err != nil {
		return nil, err
	}
	if len(files) > 0 {
		if err := checkUploadLimit(ctx, rt.client, files); err != nil {
			return nil, err
		}
	}

	id, err := rt.resolveTaskID(ctx, ref)
	if err != nil {
		return nil, err
	}
	task, err := rt.client.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	taskRef := rt.cfg.FormatTaskID(task.Index)

	res := &prResult{Base: base, Head: head, Attachments: []attachedFile{}}
	res.PRURL, err = findOpenPR(ctx, repo, head)
	if err != nil {
		return nil, err
	}
	if res.PRURL != "" {
		res.Existing = true
	} else {
		if strings.TrimSpace(f.title) == "" {
			return nil, output.New(output.CodeValidation, "--title is required to open a PR")
		}
		res.PRURL, err = createPR(ctx, repo, base, head, f.title, withRefs(body, taskRef), f.draft)
		if err != nil {
			return nil, err
		}
	}

	// From here on the PR exists; the task follows it.
	if len(files) > 0 {
		paths := make([]string, len(files))
		for i, fl := range files {
			paths[i] = fl.path
		}
		up, err := rt.client.UploadTaskAttachments(ctx, id, paths)
		if err != nil {
			return nil, output.Wrap(output.CodeUnknown, err, "PR %s is open, but attaching files to %s failed: %v", res.PRURL, taskRef, err)
		}
		for _, a := range up.Success {
			af := attachedFile{ID: a.ID}
			if a.File != nil {
				af.Name, af.Size, af.Mime = a.File.Name, a.File.Size, a.File.Mime
			}
			res.Attachments = append(res.Attachments, af)
		}
		sleepCtx(ctx, writeGap)
	}

	comment, err := prComment(res.PRURL, summary, res.Attachments)
	if err != nil {
		return nil, err
	}
	updated, err := runUpdate(ctx, rt, id, &updateFlags{statusName: "in-review", comment: comment})
	if err != nil {
		return nil, output.Wrap(output.CodeUnknown, err, "PR %s is open, but updating %s failed: %v", res.PRURL, taskRef, err)
	}
	rt.withURLs(updated)
	res.Task = updated
	return res, nil
}

func readTextFlag(inline, path, flag string) (string, error) {
	if path == "" {
		return inline, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", output.Wrap(output.CodeValidation, err, "%s %q: %v", flag, path, err)
	}
	return string(b), nil
}

// withRefs appends "Refs: <ID>" unless the body already carries it.
func withRefs(body, taskRef string) string {
	line := "Refs: " + taskRef
	for _, l := range strings.Split(body, "\n") {
		if strings.EqualFold(strings.TrimSpace(l), line) {
			return body
		}
	}
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return line + "\n"
	}
	return body + "\n\n" + line + "\n"
}

// prComment is the In Review comment: the PR link, the summary, and the
// names of files attached with it.
func prComment(prURL, summary string, attached []attachedFile) (string, error) {
	var b strings.Builder
	b.WriteString(`<p><strong>PR:</strong> <a href="` + htmlEscape(prURL) + `">` + htmlEscape(prURL) + `</a></p>`)
	if s := strings.TrimSpace(summary); s != "" {
		html, err := maybeMarkdown(s, true)
		if err != nil {
			return "", err
		}
		b.WriteString("\n" + html)
	}
	if len(attached) > 0 {
		names := make([]string, len(attached))
		for i, a := range attached {
			names[i] = "<code>" + htmlEscape(a.Name) + "</code>"
		}
		b.WriteString("\n<p><strong>Attached:</strong> " + strings.Join(names, ", ") + "</p>")
	}
	return b.String(), nil
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

func repoArgs(repo string) []string {
	if repo == "" {
		return nil
	}
	return []string{"--repo", repo}
}

// findOpenPR returns the URL of the open PR whose head is branch, or "".
func findOpenPR(ctx context.Context, repo, branch string) (string, error) {
	args := append([]string{"pr", "list", "--head", branch, "--state", "open", "--json", "url", "--limit", "1"}, repoArgs(repo)...)
	out, err := ghRunner(ctx, "", args...)
	if err != nil {
		return "", ghError(err)
	}
	var prs []struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &prs); err != nil {
		return "", output.Wrap(output.CodeUnknown, err, "parse gh pr list output: %v", err)
	}
	if len(prs) == 0 {
		return "", nil
	}
	return prs[0].URL, nil
}

// createPR opens the PR; the body goes through stdin so it never touches
// the command line. gh prints the new PR's URL as its last line.
func createPR(ctx context.Context, repo, base, head, title, body string, draft bool) (string, error) {
	args := []string{"pr", "create", "--base", base, "--head", head, "--title", title, "--body-file", "-"}
	if draft {
		args = append(args, "--draft")
	}
	args = append(args, repoArgs(repo)...)
	out, err := ghRunner(ctx, body, args...)
	if err != nil {
		return "", ghError(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	url := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(url, "https://") {
		return "", output.New(output.CodeUnknown, "gh pr create printed no PR URL: %q", out)
	}
	return url, nil
}

func ghError(err error) error {
	var exitErr *exec.Error
	if errors.As(err, &exitErr) {
		return output.Wrap(output.CodeNotConfigured, err, "gh is not installed: %v", err)
	}
	return output.Wrap(output.CodeUnknown, err, "%v", err)
}

func sleepCtx(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
