// Package shotcraft provides VAC's fixed, local Shotcraft catalog.
package shotcraft

import (
	"embed"
	"encoding/json"
	"strings"
)

const Revision = "1266a3ec4831289c4d893d42dd5102723658039a"

//go:embed catalog.json compiled.json all:upstream
var files embed.FS

type Variant struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

type Card struct {
	ID       string    `json:"id"`
	Category string    `json:"category"`
	Summary  string    `json:"summary"`
	Use      string    `json:"use"`
	Duration string    `json:"duration"`
	Recipe   string    `json:"recipe"`
	Styles   []Variant `json:"styles"`
}

type Template struct {
	Source         string   `json:"source,omitempty"`
	DurationFrames int      `json:"duration_frames,omitempty"`
	NeedsAdapter   string   `json:"needs_adapter,omitempty"`
	ImageAliases   []string `json:"image_aliases,omitempty"`
}

var cards = load[[]Card]("catalog.json")
var templates = load[map[string]Template]("compiled.json")

func load[T any](name string) T {
	raw, err := files.ReadFile(name)
	if err != nil {
		panic(err)
	}
	var data T
	if err := json.Unmarshal(raw, &data); err != nil {
		panic(err)
	}
	return data
}

func List(query string) []Card {
	query = strings.ToLower(strings.TrimSpace(query))
	out := []Card{}
	for _, card := range cards {
		if strings.Contains(strings.ToLower(card.ID+" "+card.Category+" "+card.Summary+" "+card.Use), query) {
			out = append(out, card)
		}
	}
	return out
}

func Lookup(reference string) (Card, Variant, bool) {
	id, variant, explicit := strings.Cut(reference, "/")
	for _, card := range cards {
		if card.ID != id {
			continue
		}
		for _, style := range card.Styles {
			if !explicit || style.ID == variant {
				return card, style, true
			}
		}
		break
	}
	return Card{}, Variant{}, false
}

func Compiled(card Card, variant Variant) Template {
	return templates[card.ID+"/"+variant.ID]
}

func Read(path string) (string, error) {
	body, err := files.ReadFile("upstream/" + path)
	return string(body), err
}
