package functionTools

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

func bookSearch(ctx context.Context, req struct {
	Genre     string `json:"genre" jsonschema:"description=书籍类型，例如 fantasy/romance"`
	MaxPages  int    `json:"max_pages" jsonschema:"description:Maximum page length (0 for no limit)"`
	MinRating int    `json:"min_rating" jsonschema:"description:Minimum user rating (0-5 scale)"`
}) (map[string]any, error) {
	result := map[string]any{
		"Books": []string{"《平凡的世界》"},
	}
	return result, nil
}

func GetBookSearchTool() tool.Tool {
	BookSearchTool := function.NewFunctionTool(
		bookSearch,
		function.WithName("booksearch"),
		function.WithDescription("查询图书"),
	)
	return BookSearchTool
}
