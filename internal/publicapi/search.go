package publicapi

import "errors"

// Codex advertises its hosted search descriptor when web_search is unset, even
// for custom providers. Only the client-executed MCP search is runnable here.
// Omit the eager hosted descriptor only when that concrete MCP is declared;
// never send an unsupported hosted tool to Gemini or pretend it has an executor.
func clientSearchTools(tools []any) ([]any, error) {
	available := false
	for _, value := range tools {
		tool, _ := value.(map[string]any)
		if tool["type"] == "function" && tool["name"] == "mcp__clirelay_search__web_search" {
			available = true
		}
		if tool["type"] != "namespace" || tool["name"] != "mcp__clirelay_search" {
			continue
		}
		children, _ := tool["tools"].([]any)
		for _, value := range children {
			child, _ := value.(map[string]any)
			if child["type"] == "function" && child["name"] == "web_search" {
				available = true
			}
		}
	}
	out := make([]any, 0, len(tools))
	for _, value := range tools {
		tool, _ := value.(map[string]any)
		kind, _ := tool["type"].(string)
		if kind == "web_search" || kind == "web_search_preview" {
			if !available {
				return nil, errors.New("hosted web search is unavailable; connect the clirelay_search MCP")
			}
			continue
		}
		out = append(out, value)
	}
	return out, nil
}
