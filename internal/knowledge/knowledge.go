package knowledge

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"
)

type Base struct {
	Sections []Section
}

type Section struct {
	Title   string
	Tags    []string
	Content string
}

func Load(path string) (Base, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Base{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Base{}, err
	}
	return Parse(string(data)), nil
}

func Parse(markdown string) Base {
	var sections []Section
	var current Section
	var body []string

	flush := func() {
		if current.Title == "" {
			return
		}
		current.Content = strings.TrimSpace(strings.Join(body, "\n"))
		if current.Content != "" {
			sections = append(sections, current)
		}
		current = Section{}
		body = nil
	}

	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			current.Title = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			continue
		}
		if current.Title == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "tags:") {
			current.Tags = splitTags(strings.TrimSpace(trimmed[len("Tags:"):]))
			continue
		}
		body = append(body, line)
	}
	flush()

	return Base{Sections: sections}
}

func (b Base) Relevant(query string, limit int) []Section {
	if limit < 1 {
		return nil
	}

	normalized := normalize(query)
	if normalized == "" {
		return nil
	}
	words := wordSet(normalized)

	var matches []scoredSection
	for index, section := range b.Sections {
		score := sectionScore(section, normalized, words)
		if score > 0 {
			matches = append(matches, scoredSection{
				section: section,
				index:   index,
				score:   score,
			})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].index < matches[j].index
		}
		return matches[i].score > matches[j].score
	})

	if len(matches) > limit {
		matches = matches[:limit]
	}
	result := make([]Section, 0, len(matches))
	for _, match := range matches {
		result = append(result, match.section)
	}
	return result
}

func Format(sections []Section) string {
	if len(sections) == 0 {
		return "Known facts: none selected for this request."
	}

	var out strings.Builder
	out.WriteString("Known facts selected for this request:\n")
	for _, section := range sections {
		fmt.Fprintf(&out, "## %s\n%s\n", section.Title, section.Content)
	}
	return strings.TrimSpace(out.String())
}

type scoredSection struct {
	section Section
	index   int
	score   int
}

func sectionScore(section Section, normalizedQuery string, queryWords map[string]bool) int {
	score := 0
	for _, tag := range section.Tags {
		tag = normalize(tag)
		if tag == "" {
			continue
		}
		if strings.Contains(tag, " ") {
			if strings.Contains(normalizedQuery, tag) {
				score += 4
			}
			continue
		}
		if queryWords[tag] {
			score += 3
		}
	}

	title := normalize(section.Title)
	if title != "" && strings.Contains(normalizedQuery, title) {
		score += 2
	}
	return score
}

func splitTags(raw string) []string {
	var tags []string
	for _, part := range strings.Split(raw, ",") {
		tag := strings.TrimSpace(strings.ToLower(part))
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func normalize(input string) string {
	input = strings.ToLower(input)
	var out strings.Builder
	lastSpace := true
	for _, r := range input {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			out.WriteRune(r)
			lastSpace = false
		case !lastSpace:
			out.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(out.String())
}

func wordSet(input string) map[string]bool {
	words := map[string]bool{}
	for _, word := range strings.Fields(input) {
		words[word] = true
	}
	return words
}
