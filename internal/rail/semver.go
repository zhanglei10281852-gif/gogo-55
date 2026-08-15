package rail

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type Version struct {
	Major int
	Minor int
	Patch int
	Pre   []string
	Build []string
}

type Comparator struct {
	Operator string
	Version  Version
}

type VersionRange struct {
	AnyOf [][]Comparator
	Raw   string
}

func ParseVersion(input string) (Version, error) {
	var result Version
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "v") {
		input = input[1:]
	}
	if input == "" {
		return result, fmt.Errorf("empty semantic version")
	}
	mainAndBuild := strings.SplitN(input, "+", 2)
	if len(mainAndBuild) == 2 {
		ids, err := parseIdentifiers(mainAndBuild[1], false)
		if err != nil {
			return result, fmt.Errorf("invalid build metadata: %w", err)
		}
		result.Build = ids
	}
	mainAndPre := strings.SplitN(mainAndBuild[0], "-", 2)
	if len(mainAndPre) == 2 {
		ids, err := parseIdentifiers(mainAndPre[1], true)
		if err != nil {
			return result, fmt.Errorf("invalid prerelease: %w", err)
		}
		result.Pre = ids
	}
	parts := strings.Split(mainAndPre[0], ".")
	if len(parts) != 3 {
		return result, fmt.Errorf("semantic version requires major.minor.patch")
	}
	values := make([]int, 3)
	for index, part := range parts {
		value, err := parseNumericIdentifier(part)
		if err != nil {
			return result, fmt.Errorf("invalid version component %q: %w", part, err)
		}
		values[index] = value
	}
	result.Major = values[0]
	result.Minor = values[1]
	result.Patch = values[2]
	return result, nil
}
func parseNumericIdentifier(value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("empty numeric identifier")
	}
	if len(value) > 1 && value[0] == '0' {
		return 0, fmt.Errorf("leading zero")
	}
	for _, char := range value {
		if !unicode.IsDigit(char) {
			return 0, fmt.Errorf("non-numeric character")
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 31)
	if err != nil {
		return 0, fmt.Errorf("out of range")
	}
	return int(parsed), nil
}

func parseIdentifiers(value string, enforceNumeric bool) ([]string, error) {
	if value == "" {
		return nil, fmt.Errorf("empty identifier list")
	}
	parts := strings.Split(value, ".")
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("empty identifier")
		}
		numeric := true
		for _, char := range part {
			if !unicode.IsDigit(char) {
				numeric = false
			}
			if !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '-' {
				return nil, fmt.Errorf("invalid character %q", char)
			}
		}
		if enforceNumeric && numeric && len(part) > 1 && part[0] == '0' {
			return nil, fmt.Errorf("numeric prerelease identifier has leading zero")
		}
	}
	return parts, nil
}

func (v Version) String() string {
	result := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		result += "-" + strings.Join(v.Pre, ".")
	}
	if len(v.Build) > 0 {
		result += "+" + strings.Join(v.Build, ".")
	}
	return result
}

func (v Version) Compare(other Version) int {
	if v.Major != other.Major {
		return compareInt(v.Major, other.Major)
	}
	if v.Minor != other.Minor {
		return compareInt(v.Minor, other.Minor)
	}
	if v.Patch != other.Patch {
		return compareInt(v.Patch, other.Patch)
	}
	if len(v.Pre) == 0 && len(other.Pre) == 0 {
		return 0
	}
	if len(v.Pre) == 0 {
		return 1
	}
	if len(other.Pre) == 0 {
		return -1
	}
	limit := len(v.Pre)
	if len(other.Pre) < limit {
		limit = len(other.Pre)
	}
	for index := 0; index < limit; index++ {
		left := v.Pre[index]
		right := other.Pre[index]
		if left == right {
			continue
		}
		leftNumber, leftErr := strconv.Atoi(left)
		rightNumber, rightErr := strconv.Atoi(right)
		if leftErr == nil && rightErr == nil {
			return compareInt(leftNumber, rightNumber)
		}
		if leftErr == nil {
			return -1
		}
		if rightErr == nil {
			return 1
		}
		if left < right {
			return -1
		}
		return 1
	}
	return compareInt(len(v.Pre), len(other.Pre))
}

func compareInt(left, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func (v Version) Equal(other Version) bool {
	return v.Compare(other) == 0
}

func (v Version) Less(other Version) bool {
	return v.Compare(other) < 0
}

func (v Version) Stable() bool {
	return len(v.Pre) == 0
}

func (v Version) NextMajor() Version {
	return Version{Major: v.Major + 1}
}

func (v Version) NextMinor() Version {
	return Version{Major: v.Major, Minor: v.Minor + 1}
}

func (v Version) NextPatch() Version {
	return Version{Major: v.Major, Minor: v.Minor, Patch: v.Patch + 1}
}

func ParseRange(input string) (VersionRange, error) {
	result := VersionRange{Raw: input}
	input = strings.TrimSpace(input)
	if input == "" || input == "*" || strings.EqualFold(input, "latest") {
		result.AnyOf = [][]Comparator{{}}
		return result, nil
	}
	alternatives := strings.Split(input, "||")
	for _, alternative := range alternatives {
		comparators, err := parseRangeAlternative(strings.TrimSpace(alternative))
		if err != nil {
			return VersionRange{}, fmt.Errorf("invalid range %q: %w", input, err)
		}
		result.AnyOf = append(result.AnyOf, comparators)
	}
	return result, nil
}

func parseRangeAlternative(input string) ([]Comparator, error) {
	if input == "" {
		return nil, fmt.Errorf("empty alternative")
	}
	if strings.Contains(input, " - ") {
		parts := strings.Split(input, " - ")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid hyphen range")
		}
		lower, err := ParseVersion(parts[0])
		if err != nil {
			return nil, err
		}
		upper, err := ParseVersion(parts[1])
		if err != nil {
			return nil, err
		}
		return []Comparator{{Operator: ">=", Version: lower}, {Operator: "<=", Version: upper}}, nil
	}
	tokens := strings.Fields(strings.ReplaceAll(input, ",", " "))
	result := make([]Comparator, 0, len(tokens)*2)
	for _, token := range tokens {
		expanded, err := parseComparator(token)
		if err != nil {
			return nil, err
		}
		result = append(result, expanded...)
	}
	return result, nil
}

func parseComparator(token string) ([]Comparator, error) {
	operator := "="
	for _, candidate := range []string{">=", "<=", "!=", ">", "<", "=", "^", "~"} {
		if strings.HasPrefix(token, candidate) {
			operator = candidate
			token = strings.TrimSpace(token[len(candidate):])
			break
		}
	}
	if strings.ContainsAny(token, "xX*") {
		return expandWildcard(token)
	}
	version, err := ParseVersion(token)
	if err != nil {
		return nil, err
	}
	switch operator {
	case "^":
		upper := version.NextMajor()
		if version.Major == 0 {
			upper = version.NextMinor()
			if version.Minor == 0 {
				upper = version.NextPatch()
			}
		}
		return []Comparator{{Operator: ">=", Version: version}, {Operator: "<", Version: upper}}, nil
	case "~":
		return []Comparator{{Operator: ">=", Version: version}, {Operator: "<", Version: version.NextMinor()}}, nil
	default:
		return []Comparator{{Operator: operator, Version: version}}, nil
	}
}

func expandWildcard(token string) ([]Comparator, error) {
	parts := strings.Split(token, ".")
	if len(parts) > 3 {
		return nil, fmt.Errorf("too many wildcard components")
	}
	values := []int{0, 0, 0}
	wildcardAt := -1
	for index := 0; index < 3; index++ {
		if index >= len(parts) || parts[index] == "*" || strings.EqualFold(parts[index], "x") {
			wildcardAt = index
			break
		}
		value, err := parseNumericIdentifier(parts[index])
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	if wildcardAt == 0 {
		return []Comparator{}, nil
	}
	lower := Version{Major: values[0], Minor: values[1], Patch: values[2]}
	var upper Version
	if wildcardAt == 1 {
		upper = lower.NextMajor()
	} else {
		upper = lower.NextMinor()
	}
	return []Comparator{{Operator: ">=", Version: lower}, {Operator: "<", Version: upper}}, nil
}

func (r VersionRange) Contains(version Version) bool {
	for _, alternative := range r.AnyOf {
		matches := true
		for _, comparator := range alternative {
			if !comparator.Matches(version) {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func (c Comparator) Matches(version Version) bool {
	comparison := version.Compare(c.Version)
	switch c.Operator {
	case "=":
		return comparison == 0
	case "!=":
		return comparison != 0
	case ">":
		return comparison > 0
	case ">=":
		return comparison >= 0
	case "<":
		return comparison < 0
	case "<=":
		return comparison <= 0
	default:
		return false
	}
}

func (r VersionRange) String() string {
	if r.Raw != "" {
		return r.Raw
	}
	parts := make([]string, 0, len(r.AnyOf))
	for _, alternative := range r.AnyOf {
		comparators := make([]string, 0, len(alternative))
		for _, comparator := range alternative {
			comparators = append(comparators, comparator.Operator+comparator.Version.String())
		}
		parts = append(parts, strings.Join(comparators, " "))
	}
	return strings.Join(parts, " || ")
}

func SelectHighest(versions []Version, constraint VersionRange) (Version, bool) {
	var selected Version
	found := false
	for _, version := range versions {
		if constraint.Contains(version) && (!found || selected.Less(version)) {
			selected = version
			found = true
		}
	}
	return selected, found
}
