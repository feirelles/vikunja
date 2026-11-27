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

// Package markdown converts the GitHub-flavoured markdown agents write into
// the HTML Vikunja's TipTap editor stores. The conversion is deterministic:
// the same input always yields the same HTML, so --description-replace-old
// can match what an earlier --md call wrote.
//
// TipTap differences from plain goldmark output:
//   - task lists are <ul data-type="taskList"> with
//     <li data-type="taskItem" data-checked="true|false"><p>…</p></li>;
//   - every list item wraps its text in <p>, tight lists included;
//   - raw HTML passes through, so markdown and the HTML veans always
//     accepted can be mixed.
package markdown

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(
		html.WithUnsafe(),
		renderer.WithNodeRenderers(util.Prioritized(&tiptapRenderer{}, 100)),
	),
)

// ToHTML converts markdown to TipTap-shaped HTML, without a trailing newline.
func ToHTML(src string) (string, error) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

type tiptapRenderer struct{}

func (r *tiptapRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindList, r.renderList)
	reg.Register(ast.KindListItem, r.renderListItem)
	reg.Register(ast.KindTextBlock, r.renderTextBlock)
	reg.Register(east.KindTaskCheckBox, r.renderTaskCheckBox)
}

// taskBox returns the checkbox of a task-list item, or nil.
func taskBox(item ast.Node) *east.TaskCheckBox {
	block := item.FirstChild()
	if block == nil {
		return nil
	}
	box, _ := block.FirstChild().(*east.TaskCheckBox)
	return box
}

// isTaskList reports whether every item of the list is a task item. A list
// mixing both stays a plain list (TipTap can't hold both kinds in one list).
func isTaskList(list ast.Node) bool {
	if list.ChildCount() == 0 {
		return false
	}
	for c := list.FirstChild(); c != nil; c = c.NextSibling() {
		if taskBox(c) == nil {
			return false
		}
	}
	return true
}

func (r *tiptapRenderer) renderList(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.List)
	tag := "ul"
	if n.IsOrdered() {
		tag = "ol"
	}
	if !entering {
		_, _ = w.WriteString("</" + tag + ">\n")
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString("<" + tag)
	switch {
	case !n.IsOrdered() && isTaskList(n):
		_, _ = w.WriteString(` data-type="taskList"`)
	case n.IsOrdered() && n.Start != 1:
		_, _ = w.WriteString(` start="` + itoa(n.Start) + `"`)
	}
	_, _ = w.WriteString(">\n")
	return ast.WalkContinue, nil
}

func (r *tiptapRenderer) renderListItem(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("</li>\n")
		return ast.WalkContinue, nil
	}
	if box := taskBox(node); box != nil && isTaskList(node.Parent()) {
		checked := "false"
		if box.IsChecked {
			checked = "true"
		}
		_, _ = w.WriteString(`<li data-type="taskItem" data-checked="` + checked + `">`)
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString("<li>")
	return ast.WalkContinue, nil
}

// renderTextBlock wraps the text of tight list items in <p>, like TipTap.
func (r *tiptapRenderer) renderTextBlock(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if _, inItem := node.Parent().(*ast.ListItem); !inItem {
		if !entering && node.NextSibling() != nil && node.FirstChild() != nil {
			_ = w.WriteByte('\n')
		}
		return ast.WalkContinue, nil
	}
	if entering {
		_, _ = w.WriteString("<p>")
	} else {
		_, _ = w.WriteString("</p>")
	}
	return ast.WalkContinue, nil
}

// renderTaskCheckBox drops the checkbox inside task lists (the state lives on
// the <li>) and keeps the literal marker otherwise.
func (r *tiptapRenderer) renderTaskCheckBox(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*east.TaskCheckBox)
	item := node.Parent().Parent()
	if list := item.Parent(); list != nil && isTaskList(list) {
		return ast.WalkContinue, nil
	}
	if n.IsChecked {
		_, _ = w.WriteString("[x] ")
	} else {
		_, _ = w.WriteString("[ ] ")
	}
	return ast.WalkContinue, nil
}

func itoa(i int) string {
	var b [20]byte
	pos := len(b)
	neg := i < 0
	if neg {
		i = -i
	}
	for {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
		if i == 0 {
			break
		}
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
