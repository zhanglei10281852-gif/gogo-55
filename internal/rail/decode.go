package rail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

type yamlLine struct {
	number int
	indent int
	text   string
}

type yamlParser struct {
	lines []yamlLine
	index int
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	manifest, err := DecodeManifest(data, extensionFormat(path))
	if err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return manifest, nil
}

func extensionFormat(path string) string {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		return "yaml"
	}
	return "json"
}

func DecodeManifest(data []byte, format string) (Manifest, error) {
	var manifest Manifest
	if strings.EqualFold(format, "yaml") {
		value, err := parseYAML(data)
		if err != nil {
			return manifest, err
		}
		data, err = json.Marshal(value)
		if err != nil {
			return manifest, fmt.Errorf("normalize YAML: %w", err)
		}
	}
	if err := decodeStrictJSON(data, &manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("invalid JSON: trailing data")
		}
		return fmt.Errorf("invalid JSON trailing data: %w", err)
	}
	return nil
}
func parseYAML(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("YAML is not valid UTF-8")
	}
	lines, err := tokenizeYAML(string(data))
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty YAML document")
	}
	parser := yamlParser{lines: lines}
	value, err := parser.parseBlock(lines[0].indent)
	if err != nil {
		return nil, err
	}
	if parser.index != len(lines) {
		line := lines[parser.index]
		return nil, fmt.Errorf("line %d: unexpected content", line.number)
	}
	return value, nil
}

func tokenizeYAML(input string) ([]yamlLine, error) {
	rawLines := strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n")
	result := make([]yamlLine, 0, len(rawLines))
	for index, raw := range rawLines {
		if strings.ContainsRune(raw, '\t') {
			return nil, fmt.Errorf("line %d: tabs are not allowed", index+1)
		}
		withoutComment, err := stripYAMLComment(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", index+1, err)
		}
		trimmedRight := strings.TrimRight(withoutComment, " \r")
		if strings.TrimSpace(trimmedRight) == "" || strings.TrimSpace(trimmedRight) == "---" {
			continue
		}
		if strings.TrimSpace(trimmedRight) == "..." {
			for _, rest := range rawLines[index+1:] {
				if strings.TrimSpace(rest) != "" && !strings.HasPrefix(strings.TrimSpace(rest), "#") {
					return nil, fmt.Errorf("line %d: content after document end", index+2)
				}
			}
			break
		}
		indent := len(trimmedRight) - len(strings.TrimLeft(trimmedRight, " "))
		if indent%2 != 0 {
			return nil, fmt.Errorf("line %d: indentation must use multiples of two spaces", index+1)
		}
		result = append(result, yamlLine{number: index + 1, indent: indent, text: strings.TrimSpace(trimmedRight)})
	}
	return result, nil
}

func stripYAMLComment(line string) (string, error) {
	single := false
	double := false
	escaped := false
	for index, char := range line {
		if escaped {
			escaped = false
			continue
		}
		if double && char == '\\' {
			escaped = true
			continue
		}
		if char == '\'' && !double {
			single = !single
			continue
		}
		if char == '"' && !single {
			double = !double
			continue
		}
		if char == '#' && !single && !double {
			if index == 0 || line[index-1] == ' ' {
				return line[:index], nil
			}
		}
	}
	if single || double {
		return "", fmt.Errorf("unterminated quoted scalar")
	}
	return line, nil
}

func (p *yamlParser) parseBlock(indent int) (any, error) {
	if p.index >= len(p.lines) {
		return nil, fmt.Errorf("unexpected end of YAML")
	}
	line := p.lines[p.index]
	if line.indent != indent {
		return nil, fmt.Errorf("line %d: expected indentation %d", line.number, indent)
	}
	if strings.HasPrefix(line.text, "- ") || line.text == "-" {
		return p.parseSequence(indent)
	}
	return p.parseMapping(indent)
}

func (p *yamlParser) parseMapping(indent int) (map[string]any, error) {
	result := map[string]any{}
	for p.index < len(p.lines) {
		line := p.lines[p.index]
		if line.indent < indent {
			break
		}
		if line.indent > indent {
			return nil, fmt.Errorf("line %d: unexpected indentation", line.number)
		}
		if strings.HasPrefix(line.text, "- ") || line.text == "-" {
			break
		}
		key, remainder, err := splitYAMLKey(line.text)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line.number, err)
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("line %d: duplicate key %q", line.number, key)
		}
		p.index++
		if remainder != "" {
			value, err := parseYAMLScalar(remainder)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", line.number, err)
			}
			result[key] = value
			continue
		}
		if p.index >= len(p.lines) || p.lines[p.index].indent <= indent {
			result[key] = nil
			continue
		}
		if p.lines[p.index].indent != indent+2 {
			return nil, fmt.Errorf("line %d: nested value must be indented two spaces", p.lines[p.index].number)
		}
		value, err := p.parseBlock(indent + 2)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

func (p *yamlParser) parseSequence(indent int) ([]any, error) {
	result := []any{}
	for p.index < len(p.lines) {
		line := p.lines[p.index]
		if line.indent < indent {
			break
		}
		if line.indent != indent {
			return nil, fmt.Errorf("line %d: unexpected sequence indentation", line.number)
		}
		if !strings.HasPrefix(line.text, "-") {
			break
		}
		if line.text != "-" && !strings.HasPrefix(line.text, "- ") {
			return nil, fmt.Errorf("line %d: malformed sequence item", line.number)
		}
		remainder := strings.TrimSpace(strings.TrimPrefix(line.text, "-"))
		p.index++
		if remainder == "" {
			if p.index >= len(p.lines) || p.lines[p.index].indent <= indent {
				result = append(result, nil)
				continue
			}
			value, err := p.parseBlock(indent + 2)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
			continue
		}
		if looksLikeYAMLMapping(remainder) {
			item, err := p.parseInlineSequenceMapping(line, remainder, indent)
			if err != nil {
				return nil, err
			}
			result = append(result, item)
			continue
		}
		value, err := parseYAMLScalar(remainder)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line.number, err)
		}
		if p.index < len(p.lines) && p.lines[p.index].indent > indent {
			return nil, fmt.Errorf("line %d: scalar sequence item cannot have children", p.lines[p.index].number)
		}
		result = append(result, value)
	}
	return result, nil
}

func (p *yamlParser) parseInlineSequenceMapping(line yamlLine, remainder string, indent int) (map[string]any, error) {
	result := map[string]any{}
	key, text, err := splitYAMLKey(remainder)
	if err != nil {
		return nil, fmt.Errorf("line %d: %w", line.number, err)
	}
	if text == "" {
		if p.index < len(p.lines) && p.lines[p.index].indent == indent+2 {
			value, err := p.parseBlock(indent + 2)
			if err != nil {
				return nil, err
			}
			result[key] = value
		} else {
			result[key] = nil
		}
	} else {
		value, err := parseYAMLScalar(text)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line.number, err)
		}
		result[key] = value
	}
	for p.index < len(p.lines) && p.lines[p.index].indent == indent+2 {
		child := p.lines[p.index]
		if strings.HasPrefix(child.text, "-") {
			return nil, fmt.Errorf("line %d: unexpected nested sequence", child.number)
		}
		childKey, childText, err := splitYAMLKey(child.text)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", child.number, err)
		}
		if _, exists := result[childKey]; exists {
			return nil, fmt.Errorf("line %d: duplicate key %q", child.number, childKey)
		}
		p.index++
		if childText != "" {
			value, err := parseYAMLScalar(childText)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", child.number, err)
			}
			result[childKey] = value
			continue
		}
		if p.index < len(p.lines) && p.lines[p.index].indent == indent+4 {
			value, err := p.parseBlock(indent + 4)
			if err != nil {
				return nil, err
			}
			result[childKey] = value
		} else {
			result[childKey] = nil
		}
	}
	return result, nil
}

func looksLikeYAMLMapping(value string) bool {
	_, _, err := splitYAMLKey(value)
	return err == nil
}

func splitYAMLKey(value string) (string, string, error) {
	single := false
	double := false
	escaped := false
	for index, char := range value {
		if escaped {
			escaped = false
			continue
		}
		if double && char == '\\' {
			escaped = true
			continue
		}
		if char == '\'' && !double {
			single = !single
		}
		if char == '"' && !single {
			double = !double
		}
		if char == ':' && !single && !double {
			key := strings.TrimSpace(value[:index])
			if key == "" {
				return "", "", fmt.Errorf("empty mapping key")
			}
			if strings.HasPrefix(key, "[") || strings.HasPrefix(key, "{") {
				return "", "", fmt.Errorf("complex mapping keys are unsupported")
			}
			if strings.HasPrefix(key, "\"") || strings.HasPrefix(key, "'") {
				parsed, err := parseYAMLScalar(key)
				if err != nil {
					return "", "", err
				}
				text, ok := parsed.(string)
				if !ok {
					return "", "", fmt.Errorf("mapping key must be a string")
				}
				key = text
			}
			return key, strings.TrimSpace(value[index+1:]), nil
		}
	}
	return "", "", fmt.Errorf("expected key: value mapping")
}

func parseYAMLScalar(value string) (any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if strings.HasPrefix(value, "[") {
		return parseFlowSequence(value)
	}
	if strings.HasPrefix(value, "{") {
		return parseFlowMapping(value)
	}
	if strings.HasPrefix(value, "\"") {
		var result string
		if err := json.Unmarshal([]byte(value), &result); err != nil {
			return nil, fmt.Errorf("invalid double-quoted scalar: %w", err)
		}
		return result, nil
	}
	if strings.HasPrefix(value, "'") {
		if len(value) < 2 || !strings.HasSuffix(value, "'") {
			return nil, fmt.Errorf("unterminated single-quoted scalar")
		}
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'"), nil
	}
	switch strings.ToLower(value) {
	case "null", "~":
		return nil, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	if integer, err := strconv.ParseInt(value, 10, 64); err == nil {
		return integer, nil
	}
	if decimal, err := strconv.ParseFloat(value, 64); err == nil && strings.ContainsAny(value, ".eE") {
		return decimal, nil
	}
	if strings.Contains(value, ": ") {
		return nil, fmt.Errorf("plain scalar containing ': ' must be quoted")
	}
	return value, nil
}

func splitFlow(value string, open, close byte) ([]string, error) {
	if len(value) < 2 || value[0] != open || value[len(value)-1] != close {
		return nil, fmt.Errorf("malformed flow value")
	}
	body := strings.TrimSpace(value[1 : len(value)-1])
	if body == "" {
		return []string{}, nil
	}
	var result []string
	start := 0
	depth := 0
	single := false
	double := false
	escaped := false
	for index, char := range body {
		if escaped {
			escaped = false
			continue
		}
		if double && char == '\\' {
			escaped = true
			continue
		}
		if char == '\'' && !double {
			single = !single
			continue
		}
		if char == '"' && !single {
			double = !double
			continue
		}
		if single || double {
			continue
		}
		switch char {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case ',':
			if depth == 0 {
				result = append(result, strings.TrimSpace(body[start:index]))
				start = index + 1
			}
		}
		if depth < 0 {
			return nil, fmt.Errorf("unbalanced flow value")
		}
	}
	if single || double || depth != 0 {
		return nil, fmt.Errorf("unbalanced flow value")
	}
	result = append(result, strings.TrimSpace(body[start:]))
	return result, nil
}

func parseFlowSequence(value string) ([]any, error) {
	parts, err := splitFlow(value, '[', ']')
	if err != nil {
		return nil, err
	}
	result := make([]any, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("empty flow sequence item")
		}
		item, err := parseYAMLScalar(part)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func parseFlowMapping(value string) (map[string]any, error) {
	parts, err := splitFlow(value, '{', '}')
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	for _, part := range parts {
		key, text, err := splitYAMLKey(part)
		if err != nil {
			return nil, err
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate flow mapping key %q", key)
		}
		item, err := parseYAMLScalar(text)
		if err != nil {
			return nil, err
		}
		result[key] = item
	}
	return result, nil
}
