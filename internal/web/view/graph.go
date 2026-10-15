package view

import "github.com/O-Marsters-1997/command-center/internal/option"

type GraphGroup struct {
	Root     option.Option[Row] `json:"root"`
	Children []Row              `json:"children"`
}

func Graph(groups []Group) []GraphGroup {
	result := make([]GraphGroup, 0, len(groups))
	for _, group := range groups {
		converted := GraphGroup{Children: make([]Row, 0, len(group.Children))}
		if group.Root.IsSome() {
			root := group.Root.Unwrap()
			root.Ref = root.Ticket()
			root.SessionPath = SessionPath(root.URL)
			converted.Root = option.Some(root)
		}
		for _, child := range group.Children {
			child.Ref = child.Ticket()
			child.SessionPath = SessionPath(child.URL)
			converted.Children = append(converted.Children, child)
		}
		result = append(result, converted)
	}
	return result
}
