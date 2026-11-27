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

package mcp

// Compact responses: list and write tools return a short summary of each
// object unless the caller asks for fields. Single reads stay complete.

const (
	// allFields in a fields argument returns the complete response.
	allFields = "*"
	// listRichTextLimit is the number of characters a rich-text field keeps in list results.
	listRichTextLimit = 200
)

type compactMode int

const (
	compactNone compactMode = iota
	compactList
	compactWrite
)

// richTextFields are echoed back by writes and can be long; writes drop them and lists truncate them.
var richTextFields = map[string]bool{
	"description": true,
	"comment":     true,
}

// compactTaskFields is the summary of a task in compact responses.
var compactTaskFields = parseFields([]string{
	"id",
	"identifier",
	"title",
	"done",
	"project_id",
	"bucket_id",
	"due_date",
	"labels.title",
})

// summaryFields name a nested object; the rest of it is dropped.
var summaryFields = []string{"id", "title", "username", "name"}

// isTask recognizes a task by the fields only tasks carry together.
func isTask(obj map[string]any) bool {
	_, identifier := obj["identifier"]
	_, index := obj["index"]
	_, done := obj["done"]
	return identifier && index && done
}

func compactValue(v any, mode compactMode) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = compactValue(e, mode)
		}
		return out
	case map[string]any:
		return compactObject(x, mode)
	}
	return v
}

// compactObject keeps the scalar fields of a top-level object and a summary of
// each nested object. Arrays are dropped, except a task's label titles.
func compactObject(obj map[string]any, mode compactMode) map[string]any {
	if isTask(obj) {
		return compactTaskFields.apply(obj).(map[string]any)
	}
	out := make(map[string]any, len(obj))
	for k, val := range obj {
		switch x := val.(type) {
		case map[string]any:
			out[k] = summarize(x)
		case []any:
			continue
		case string:
			if !richTextFields[k] {
				out[k] = x
				continue
			}
			if mode == compactList {
				out[k] = truncate(x, listRichTextLimit)
			}
		default:
			out[k] = val
		}
	}
	return out
}

func summarize(obj map[string]any) map[string]any {
	if isTask(obj) {
		return compactTaskFields.apply(obj).(map[string]any)
	}
	out := map[string]any{}
	for _, k := range summaryFields {
		if val, ok := obj[k]; ok {
			out[k] = val
		}
	}
	return out
}

func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

// wantsAll reports whether a fields argument asks for the complete response.
func (t fieldTree) wantsAll() bool {
	sub, ok := t[allFields]
	return ok && sub == nil
}

// shape applies the fields argument, or the compact default when there is none.
func (s *toolSpec) shape(fields fieldTree, result any) any {
	switch {
	case s.projection == noProjection, fields.wantsAll():
		return result
	case fields != nil:
		return s.projection.apply(fields, result)
	}
	mode := s.compact
	if mode == compactNone {
		// A plain array from a read is a list.
		if _, isList := result.([]any); !isList {
			return result
		}
		mode = compactList
	}
	if s.projection == projectItems {
		envelope, ok := result.(map[string]any)
		if !ok {
			return result
		}
		out := make(map[string]any, len(envelope))
		for k, v := range envelope {
			out[k] = v
		}
		out["items"] = compactValue(envelope["items"], mode)
		return out
	}
	return compactValue(result, mode)
}
