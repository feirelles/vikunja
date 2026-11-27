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

package markdown

import "testing"

func TestToHTML(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"heading and inline", "## Summary\n\nSome **bold**, *em*, `code` and [a link](https://example.com).",
			"<h2>Summary</h2>\n<p>Some <strong>bold</strong>, <em>em</em>, <code>code</code> and <a href=\"https://example.com\">a link</a>.</p>"},
		{"bullet list", "- one\n- two",
			"<ul>\n<li><p>one</p></li>\n<li><p>two</p></li>\n</ul>"},
		{"ordered list", "1. one\n2. two",
			"<ol>\n<li><p>one</p></li>\n<li><p>two</p></li>\n</ol>"},
		{"ordered list start", "3. three",
			"<ol start=\"3\">\n<li><p>three</p></li>\n</ol>"},
		{"task list", "- [ ] find the bug\n- [x] write the test",
			"<ul data-type=\"taskList\">\n<li data-type=\"taskItem\" data-checked=\"false\"><p>find the bug</p></li>\n<li data-type=\"taskItem\" data-checked=\"true\"><p>write the test</p></li>\n</ul>"},
		{"mixed list stays plain", "- [ ] task\n- plain",
			"<ul>\n<li><p>[ ] task</p></li>\n<li><p>plain</p></li>\n</ul>"},
		{"nested task list", "- parent\n  - [ ] child",
			"<ul>\n<li><p>parent</p><ul data-type=\"taskList\">\n<li data-type=\"taskItem\" data-checked=\"false\"><p>child</p></li>\n</ul>\n</li>\n</ul>"},
		{"fenced code", "```go\nif a < b {\n\treturn\n}\n```",
			"<pre><code class=\"language-go\">if a &lt; b {\n\treturn\n}\n</code></pre>"},
		{"bare URL becomes a link (task pill)", "Related: https://tasks.example.com/tasks/601",
			"<p>Related: <a href=\"https://tasks.example.com/tasks/601\">https://tasks.example.com/tasks/601</a></p>"},
		{"escapes text", "a & b < c", "<p>a &amp; b &lt; c</p>"},
		{"raw HTML passes through", "see <code>x</code>", "<p>see <code>x</code></p>"},
		{"quote and rule", "> quoted\n\n---", "<blockquote>\n<p>quoted</p>\n</blockquote>\n<hr>"},
		{"paragraphs", "one\n\ntwo", "<p>one</p>\n<p>two</p>"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ToHTML(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("\nin   %q\nwant %q\ngot  %q", tc.in, tc.want, got)
			}
		})
	}
}

// Same input, same bytes: --description-replace-old relies on it.
func TestToHTML_Deterministic(t *testing.T) {
	const in = "## T\n\n- [ ] a\n- [x] b\n\n```\ncode\n```\n"
	first, _ := ToHTML(in)
	for range 20 {
		if got, _ := ToHTML(in); got != first {
			t.Fatalf("output changed:\n%q\n%q", first, got)
		}
	}
}
