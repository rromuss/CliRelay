package publicapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type binding struct {
	Name, Namespace string
	Custom          bool
}
type Session struct {
	bindings  map[string]binding
	wires     map[string]string
	items     map[string]string
	arguments map[string]string
}

func NewSession() *Session {
	return &Session{bindings: map[string]binding{}, wires: map[string]string{}, items: map[string]string{}, arguments: map[string]string{}}
}
func identity(ns, name string) string { return ns + "\x00" + name }
func (s *Session) tools(raw []any, ns string) ([]any, error) {
	out := []any{}
	for _, value := range raw {
		t, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("invalid tool definition")
		}
		kind, _ := t["type"].(string)
		name, _ := t["name"].(string)
		if name == "" {
			return nil, errors.New("tool name is required")
		}
		if kind == "namespace" {
			if ns != "" {
				return nil, errors.New("nested tool namespaces are unsupported")
			}
			children, ok := t["tools"].([]any)
			if !ok {
				return nil, errors.New("namespace tools are required")
			}
			flat, err := s.tools(children, name)
			if err != nil {
				return nil, err
			}
			out = append(out, flat...)
			continue
		}
		if kind != "function" && kind != "custom" {
			return nil, fmt.Errorf("unsupported tool type %q", kind)
		}
		key := identity(ns, name)
		if _, ok := s.wires[key]; ok {
			return nil, errors.New("duplicate tool identity")
		}
		digest := sha256.Sum256([]byte(key))
		wire := "ext_" + hex.EncodeToString(digest[:16])
		s.wires[key] = wire
		s.bindings[wire] = binding{name, ns, kind == "custom"}
		copy := map[string]any{}
		for k, v := range t {
			copy[k] = v
		}
		copy["name"] = wire
		copy["type"] = "function"
		if kind == "custom" {
			copy["parameters"] = map[string]any{"type": "object", "properties": map[string]any{"input": map[string]any{"type": "string"}}, "required": []string{"input"}, "additionalProperties": false}
			format, _ := json.Marshal(t["format"])
			copy["description"] = fmt.Sprint(t["description"]) + " Supply the exact raw tool input in the input field. Input format: " + string(format)
			delete(copy, "format")
		}
		out = append(out, copy)
	}
	return out, nil
}

// Normalize is stateless across conversations: aliases are deterministic and
// each request carries its own original tool definitions and history.
func (s *Session) Normalize(raw []byte) ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		return nil, errors.New("invalid JSON request")
	}
	if root["model"] != Model {
		return nil, errors.New("model is not available on the external API")
	}
	if v, ok := root["previous_response_id"]; ok && v != nil && v != "" {
		return nil, errors.New("send replayed input; previous_response_id is unsupported")
	}
	if v, ok := root["background"].(bool); ok && v {
		return nil, errors.New("background responses are unsupported")
	}
	reasoning, ok := root["reasoning"].(map[string]any)
	if !ok {
		reasoning = map[string]any{}
	}
	if effort, ok := reasoning["effort"]; ok && effort != "medium" {
		return nil, errors.New("only medium reasoning is supported")
	}
	reasoning["effort"] = "medium"
	root["reasoning"] = reasoning
	root["store"] = false
	if value, exists := root["tools"]; exists && value != nil {
		if _, ok := value.([]any); !ok {
			return nil, errors.New("tools must be an array")
		}
	}
	if tools, ok := root["tools"].([]any); ok {
		flat, err := s.tools(tools, "")
		if err != nil {
			return nil, err
		}
		root["tools"] = flat
	}
	if choice, ok := root["tool_choice"].(map[string]any); ok {
		name, _ := choice["name"].(string)
		ns, _ := choice["namespace"].(string)
		wire, ok := s.wires[identity(ns, name)]
		if !ok {
			return nil, errors.New("unknown tool choice")
		}
		root["tool_choice"] = map[string]any{"type": "function", "name": wire}
	}
	if input, ok := root["input"].([]any); ok {
		for _, value := range input {
			item, ok := value.(map[string]any)
			if !ok {
				return nil, errors.New("invalid input item")
			}
			kind, _ := item["type"].(string)
			switch kind {
			case "", "message", "reasoning", "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output":
			default:
				return nil, errors.New("unsupported history item type")
			}
			if kind == "message" || kind == "" {
				if content, ok := item["content"].([]any); ok {
					for _, value := range content {
						part, ok := value.(map[string]any)
						if !ok {
							return nil, errors.New("invalid message content")
						}
						typ, _ := part["type"].(string)
						if typ != "input_text" && typ != "output_text" && typ != "" {
							return nil, errors.New("external v1 accepts text input only")
						}
					}
				}
			}
			if kind == "custom_tool_call" || kind == "function_call" {
				name, _ := item["name"].(string)
				ns, _ := item["namespace"].(string)
				wire, ok := s.wires[identity(ns, name)]
				if !ok {
					return nil, errors.New("history tool must be present in tools")
				}
				item["name"] = wire
				delete(item, "namespace")
				if kind == "custom_tool_call" {
					args, _ := json.Marshal(map[string]any{"input": item["input"]})
					item["type"] = "function_call"
					item["arguments"] = string(args)
					delete(item, "input")
				}
			} else if kind == "custom_tool_call_output" {
				item["type"] = "function_call_output"
			}
		}
	}
	return json.Marshal(root)
}

func (s *Session) restoreItem(item map[string]any) {
	name, _ := item["name"].(string)
	b, ok := s.bindings[name]
	if !ok {
		return
	}
	item["name"] = b.Name
	if b.Namespace != "" {
		item["namespace"] = b.Namespace
	}
	if id, ok := item["id"].(string); ok {
		s.items[id] = name
	}
	if b.Custom {
		item["type"] = "custom_tool_call"
		args, _ := item["arguments"].(string)
		input := ""
		if args != "" {
			var obj struct {
				Input string `json:"input"`
			}
			if json.Unmarshal([]byte(args), &obj) == nil {
				input = obj.Input
			}
		}
		item["input"] = input
		delete(item, "arguments")
	}
}

func (s *Session) restore(root map[string]any) []map[string]any {
	if item, ok := root["item"].(map[string]any); ok {
		s.restoreItem(item)
	}
	response := root
	if r, ok := root["response"].(map[string]any); ok {
		response = r
	}
	if output, ok := response["output"].([]any); ok {
		for _, v := range output {
			if item, ok := v.(map[string]any); ok {
				s.restoreItem(item)
			}
		}
	}
	kind, _ := root["type"].(string)
	id, _ := root["item_id"].(string)
	if b, ok := s.bindings[s.items[id]]; ok && b.Custom {
		if kind == "response.function_call_arguments.delta" {
			delta, _ := root["delta"].(string)
			s.arguments[id] += delta
			return nil // JSON string escapes may span chunks; emit raw input once complete.
		}
		if kind == "response.function_call_arguments.done" {
			args, _ := root["arguments"].(string)
			if args == "" {
				args = s.arguments[id]
			}
			delete(s.arguments, id)
			var obj struct {
				Input string `json:"input"`
			}
			if json.Unmarshal([]byte(args), &obj) != nil {
				return []map[string]any{{"type": "error", "error": map[string]any{"code": "invalid_tool_arguments", "message": "Invalid custom tool input"}}}
			}
			delete(root, "arguments")
			root["type"] = "response.custom_tool_call_input.done"
			root["input"] = obj.Input
			delta := map[string]any{}
			for k, v := range root {
				delta[k] = v
			}
			delta["type"] = "response.custom_tool_call_input.delta"
			delete(delta, "input")
			delta["delta"] = obj.Input
			// Gemini's converter assigns monotonic sequence numbers. Keep its
			// terminal done number and use the preceding suppressed delta number.
			if seq, ok := delta["sequence_number"].(float64); ok {
				delta["sequence_number"] = seq - 1
			}
			return []map[string]any{delta, root}
		}
	}
	return []map[string]any{root}
}

func (s *Session) RestoreJSON(raw string) string {
	var root map[string]any
	if json.Unmarshal([]byte(raw), &root) != nil {
		return raw
	}
	s.restore(root)
	out, _ := json.Marshal(root)
	return string(out)
}

func (s *Session) RestoreSSE(events []string) []string {
	out := []string{}
	for _, event := range events {
		for _, block := range strings.Split(event, "\n\n") {
			var data string
			for _, line := range strings.Split(block, "\n") {
				if strings.HasPrefix(line, "data:") {
					data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				}
			}
			if data == "" || data == "[DONE]" {
				if block != "" {
					out = append(out, block+"\n\n")
				}
				continue
			}
			var root map[string]any
			if json.Unmarshal([]byte(data), &root) != nil {
				out = append(out, block+"\n\n")
				continue
			}
			for _, restored := range s.restore(root) {
				raw, _ := json.Marshal(restored)
				kind, _ := restored["type"].(string)
				out = append(out, "event: "+kind+"\ndata: "+string(raw)+"\n\n")
			}
		}
	}
	return out
}
