package htmltest

import (
	"bytes"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/net/html"
)

type Node struct{ n *html.Node }

func Parse(tb testing.TB, body string) Node {
	tb.Helper()
	n, err := html.Parse(strings.NewReader(body))
	if err != nil {
		tb.Fatalf("parse HTML: %v", err)
	}
	return Node{n}
}

func (n Node) Find(sel string) []Node {
	parts := splitSelector(sel)
	if len(parts) == 0 {
		return nil
	}
	parents := []Node{n}
	for _, part := range parts {
		var next []Node
		seen := make(map[*html.Node]bool)
		for _, parent := range parents {
			var visit func(*html.Node)
			visit = func(node *html.Node) {
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					if seen[child] {
						continue
					}
					seen[child] = true
					if matches(child, part) {
						next = append(next, Node{child})
					}
					visit(child)
				}
			}
			visit(parent.n)
		}
		parents = next
	}
	return parents
}

func (n Node) One(tb testing.TB, sel string) Node {
	tb.Helper()
	nodes := n.Find(sel)
	if len(nodes) != 1 {
		tb.Fatalf("Find(%q) found %d elements, want one in:\n%s", sel, len(nodes), n.HTML())
	}
	return nodes[0]
}

func (n Node) Attr(name string) (string, bool) {
	for _, attr := range n.n.Attr {
		if attr.Key == name {
			return attr.Val, true
		}
	}
	return "", false
}

func (n Node) Text() string {
	var text strings.Builder
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(n.n)
	return strings.Join(strings.Fields(text.String()), " ")
}

func (n Node) HTML() string {
	var out bytes.Buffer
	if n.n != nil {
		if err := html.Render(&out, n.n); err != nil {
			panic(err)
		}
	}
	return out.String()
}

func splitSelector(sel string) []string {
	var parts []string
	start, bracket := 0, 0
	var quote rune
	for i, r := range sel {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '[':
			bracket++
		case r == ']':
			bracket--
		case unicode.IsSpace(r) && bracket == 0:
			if start < i {
				parts = append(parts, sel[start:i])
			}
			start = i + len(string(r))
		}
	}
	if start < len(sel) {
		parts = append(parts, sel[start:])
	}
	return parts
}

func matches(node *html.Node, part string) bool {
	if node.Type != html.ElementNode {
		return false
	}
	for len(part) > 0 {
		end := strings.IndexAny(part, "#. [")
		if end < 0 {
			end = len(part)
		}
		if end > 0 {
			if node.Data != part[:end] {
				return false
			}
			part = part[end:]
			continue
		}
		switch part[0] {
		case '#', '.':
			end = strings.IndexAny(part[1:], "#.[")
			if end < 0 {
				end = len(part)
			} else {
				end++
			}
			name := part[1:end]
			if part[0] == '#' {
				value, ok := (Node{node}).Attr("id")
				if !ok || value != name {
					return false
				}
			} else {
				value, _ := (Node{node}).Attr("class")
				if !strings.Contains(" "+strings.Join(strings.Fields(value), " ")+" ", " "+name+" ") {
					return false
				}
			}
			part = part[end:]
		case '[':
			end = strings.IndexByte(part, ']')
			if end < 0 {
				return false
			}
			attr := part[1:end]
			name, value, hasValue := strings.Cut(attr, "=")
			got, ok := (Node{node}).Attr(name)
			if !ok || hasValue && got != strings.Trim(value, `"'`) {
				return false
			}
			part = part[end+1:]
		default:
			return false
		}
	}
	return true
}
