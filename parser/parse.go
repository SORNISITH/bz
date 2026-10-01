package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// ---------- small HTML helpers ----------

func hasClass(n *html.Node, class string) bool {
	if n.Type != html.ElementNode {
		return false
	}
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, c := range strings.Fields(a.Val) {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func text(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(text(c))
	}
	return strings.TrimSpace(sb.String())
}

// findAll returns every node with the class (does not go inside a match)
func findAll(n *html.Node, class string) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if hasClass(x, class) {
			out = append(out, x)
			return
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

func findFirst(n *html.Node, class string) *html.Node {
	if r := findAll(n, class); len(r) > 0 {
		return r[0]
	}
	return nil
}

// ---------- main parse function ----------

var eventRe = regexp.MustCompile(`/line/[^/]+/\d+-([^/]+)/`)

// eventName: "/en/line/ufc/3072381-ufc-332/..." -> "ufc 332"
func eventName(href string) string {
	m := eventRe.FindStringSubmatch(href)
	if m == nil {
		return "unknown"
	}
	return strings.ReplaceAll(m[1], "-", " ")
}

type OutPlayer struct {
	Name string  `json:"name"`
	Odd  float64 `json:"odd"`
}

type OutFile struct {
	BetName string        `json:"bet_name"`
	Bet     float64       `json:"bet"`
	Groups  [][]OutPlayer `json:"groups"`
}

// ParseFights reads the fight-card HTML and returns the data in your JSON format.
func ParseFights(r io.Reader, bet float64) (OutFile, error) {
	out := OutFile{BetName: "unknown", Bet: bet}

	doc, err := html.Parse(r)
	if err != nil {
		return out, fmt.Errorf("bad html: %w", err)
	}

	cards := findAll(doc, "ufc-fight-card")
	if len(cards) == 0 {
		return out, fmt.Errorf("no fight cards found")
	}

	// event name from the first card link
	if link := findFirst(cards[0], "ufc-fight-card__link"); link != nil {
		out.BetName = eventName(attr(link, "href"))
	}

	for _, card := range cards {
		// two fighter names
		var names []string
		for _, n := range findAll(card, "ufc-fighter__name") {
			names = append(names, text(n))
		}

		// W1 / W2 odds (skip X and the "+9" button)
		odds := map[string]float64{}
		for _, m := range findAll(card, "ufc-fight-markets__market") {
			nameNode := findFirst(m, "ufc-fight-markets__name")
			valNode := findFirst(m, "ui-market__value")
			if nameNode == nil || valNode == nil {
				continue
			}
			key := text(nameNode)
			if key != "W1" && key != "W2" {
				continue
			}
			v, err := strconv.ParseFloat(text(valNode), 64)
			if err != nil {
				continue
			}
			odds[key] = v
		}

		if len(names) != 2 || odds["W1"] == 0 || odds["W2"] == 0 {
			continue // incomplete card (no odds yet)
		}

		out.Groups = append(out.Groups, []OutPlayer{
			{Name: names[0], Odd: odds["W1"]},
			{Name: names[1], Odd: odds["W2"]},
		})
	}

	if len(out.Groups) == 0 {
		return out, fmt.Errorf("no fights with odds found")
	}
	return out, nil
}

// ParseFile reads an html file and writes the json file
func ParseFile(htmlPath, jsonPath string, bet float64) error {
	f, err := os.Open(htmlPath)
	if err != nil {
		return err
	}
	defer f.Close()

	res, err := ParseFights(f, bet)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(jsonPath, data, 0o644)
}



func main() {
	// usage: go run . parse ufc.html data.json
	if len(os.Args) < 4 || os.Args[1] != "parse" {
		fmt.Println("usage: go run . parse <input.html> <output.json>")
		fmt.Println("example: go run . parse ufc.html data.json")
		os.Exit(1)
	}

	in, out := os.Args[2], os.Args[3]

	if err := ParseFile(in, out, 1.0); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	// read it back to confirm
	data, _ := os.ReadFile(out)
	var res OutFile
	json.Unmarshal(data, &res)
	fmt.Printf("saved %s | event: %s | %d fights\n", out, res.BetName, len(res.Groups))
}
