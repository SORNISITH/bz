package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"golang.org/x/net/html"
)

const DefaultFile = "data.json"
const InitBet float64 = 1.0 // $

// =====================================================
// DATA TYPES
// =====================================================

type Player struct {
	Name string  `json:"name"`
	Odd  float64 `json:"odd"`
	Fair float64 `json:"-"` // calculated, not read from the file
}

type Input struct {
	BetName string     `json:"bet_name"`
	Bet     float64    `json:"bet"`
	Groups  [][]Player `json:"groups"`
}

func (p Player) Implied() float64 { return 1 / p.Odd }

func (p Player) Price(bet float64) float64 { return round2(bet * p.Odd) }

func round2(x float64) float64 { return math.Round(x*100) / 100 }

// Normalize: removes the bookmaker margin, fair chances of a group add up to 100%
func Normalize(group []Player) []Player {
	sum := 0.0
	for _, p := range group {
		sum += 1 / p.Odd
	}
	out := make([]Player, len(group))
	for i, p := range group {
		p.Fair = (1 / p.Odd) / sum
		out[i] = p
	}
	return out
}

type Vtype string

const (
	Equal Vtype = "Equal"
	Diff  Vtype = "Diff"
)

type Versus struct {
	Opponents []Player
	Type      Vtype
	Bet       float64
	TotalOdd  float64
	WinProb   float64
	Price     float64
	Profit    float64
}

type MatchList []Versus

func NewVersus(players []Player, bet float64) Versus {
	t := Equal
	totalOdd := 1.0
	prob := 1.0

	for _, p := range players {
		if p.Odd != players[0].Odd {
			t = Diff
		}
		totalOdd *= p.Odd
		prob *= p.Fair
	}

	totalOdd = round2(totalOdd)
	price := round2(bet * totalOdd)

	return Versus{
		Opponents: players,
		Type:      t,
		Bet:       bet,
		TotalOdd:  totalOdd,
		WinProb:   prob,
		Price:     price,
		Profit:    round2(price - bet),
	}
}

// "Court McGee : Marvin Vettori"
func (v Versus) Name() string {
	names := make([]string, len(v.Opponents))
	for i, p := range v.Opponents {
		names[i] = p.Name
	}
	return strings.Join(names, " : ")
}

func (v Versus) WinPercent() float64 { return v.WinProb * 100 }

func (v Versus) Min() float64 {
	m := v.Opponents[0].Odd
	for _, p := range v.Opponents {
		m = min(m, p.Odd)
	}
	return m
}

func (v Versus) Max() float64 {
	m := v.Opponents[0].Odd
	for _, p := range v.Opponents {
		m = max(m, p.Odd)
	}
	return m
}

// =====================================================
// STACKS (one player from EACH group)
// =====================================================

func Stacks(bet float64, groups ...[]Player) MatchList {
	if len(groups) == 0 {
		return nil
	}

	norm := make([][]Player, len(groups))
	for i, g := range groups {
		norm[i] = Normalize(g)
	}

	combos := [][]Player{{}}
	for _, g := range norm {
		var next [][]Player
		for _, c := range combos {
			for _, p := range g {
				n := make([]Player, len(c), len(c)+1) // copy, avoid shared slices
				copy(n, c)
				next = append(next, append(n, p))
			}
		}
		combos = next
	}

	out := make(MatchList, 0, len(combos))
	for _, c := range combos {
		out = append(out, NewVersus(c, bet))
	}
	return out
}

func (h MatchList) SortStrongest() {
	slices.SortStableFunc(h, func(a, b Versus) int {
		return cmp.Or(
			cmp.Compare(b.Min(), a.Min()),
			cmp.Compare(b.Max(), a.Max()),
		)
	})
}

func (h MatchList) SortByWinProb() {
	slices.SortStableFunc(h, func(a, b Versus) int {
		return cmp.Compare(b.WinProb, a.WinProb)
	})
}

func (h MatchList) SortByPrice() {
	slices.SortStableFunc(h, func(a, b Versus) int {
		return cmp.Compare(b.Price, a.Price)
	})
}

func (h MatchList) Filter(t Vtype) MatchList {
	var out MatchList
	for _, v := range h {
		if v.Type == t {
			out = append(out, v)
		}
	}
	return out
}

// =====================================================
// PRINTING
// =====================================================

func PrintFair(w io.Writer, bet float64, groups ...[]Player) {
	for i, g := range groups {
		sum := 0.0
		for _, p := range g {
			sum += p.Implied()
		}
		fmt.Fprintf(w, "\t Group %d | margin %.1f%%\n", i+1, (sum-1)*100)

		for _, p := range Normalize(g) {
			fmt.Fprintf(w, "\t   %s [ %.3f ] -> $%.2f | implied %.1f%% | fair %.1f%%\n",
				p.Name, p.Odd, p.Price(bet), p.Implied()*100, p.Fair*100)
		}
	}
	fmt.Fprintf(w, "\n")
}

func (h MatchList) Print(w io.Writer) {
	for _, v := range h {
		fmt.Fprintf(w, "%s \n", v.Name())
		fmt.Fprintf(w, " ->  odd=%-8.2f -> W=%6.2f%% -> bet=$%.2f -> price=$%.2f\n\n",
			v.TotalOdd, v.WinPercent(), v.Bet, v.Price)
	}
}

// PrintAndSave writes to the screen AND to a txt file
func (h MatchList) PrintAndSave(in Input) error {
	safe := strings.NewReplacer(":", "", "/", "-", "\\", "-", " ", "_").Replace(in.BetName)
	if safe == "" {
		safe = "bet"
	}
	filename := fmt.Sprintf("%s_%s.txt", safe, time.Now().Format("20060102_150405"))

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := io.MultiWriter(os.Stdout, f)

	PrintFair(w, in.Bet, in.Groups...)
	h.Print(w)

	fmt.Println("saved", filename)
	return nil
}

// =====================================================
// READ JSON
// =====================================================

func LoadInput(path string) (Input, error) {
	var in Input

	data, err := os.ReadFile(path)
	if err != nil {
		return in, fmt.Errorf("cannot read %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return in, fmt.Errorf("bad JSON in %q: %w", path, err)
	}

	if in.Bet <= 0 {
		in.Bet = InitBet
	}
	if len(in.Groups) == 0 {
		return in, fmt.Errorf("no groups in %q", path)
	}
	for i, g := range in.Groups {
		if len(g) == 0 {
			return in, fmt.Errorf("group %d is empty", i+1)
		}
		for _, p := range g {
			if p.Name == "" {
				return in, fmt.Errorf("group %d has a player without a name", i+1)
			}
			if p.Odd <= 1 {
				return in, fmt.Errorf("player %s in group %d: odd must be greater than 1 (got %v)", p.Name, i+1, p.Odd)
			}
		}
	}
	return in, nil
}

// =====================================================
// CHAIN ANALYSIS
// =====================================================

type ChainPlan struct {
	GroupIdx    []int     // which groups (fights) are in the chain
	Bets        MatchList // the m combos you bet, best payout first
	Stake       float64   // bet * m
	WorstReturn float64   // lowest payout among your bets
	WorstRatio  float64   // WorstReturn / Stake (1.0 = all money back)
	CoverProb   float64   // chance that one of your m bets wins
	ExpReturn   float64   // expected return / stake (margin removed)
}

// all ways to pick k indexes from 0..n-1
func combinations(n, k int) [][]int {
	var out [][]int
	var cur []int
	var rec func(start int)
	rec = func(start int) {
		if len(cur) == k {
			out = append(out, slices.Clone(cur))
			return
		}
		for i := start; i < n; i++ {
			cur = append(cur, i)
			rec(i + 1)
			cur = cur[:len(cur)-1]
		}
	}
	rec(0)
	return out
}

// k = fights in one chain, m = combos you bet, top = plans to return
func AnalyzeChains(groups [][]Player, bet float64, k, m, top int) []ChainPlan {
	if k < 1 || k > len(groups) || m < 1 {
		return nil
	}

	var plans []ChainPlan
	for _, idx := range combinations(len(groups), k) {
		sub := make([][]Player, k)
		for i, gi := range idx {
			sub[i] = groups[gi]
		}

		all := Stacks(bet, sub...)
		all.SortByPrice() // biggest payout first

		mm := min(m, len(all))
		bets := all[:mm]

		cover, exp := 0.0, 0.0
		for _, v := range bets {
			cover += v.WinProb
			exp += v.WinProb * v.Price
		}
		stake := bet * float64(mm)
		worst := bets[mm-1].Price

		plans = append(plans, ChainPlan{
			GroupIdx:    idx,
			Bets:        slices.Clone(bets),
			Stake:       stake,
			WorstReturn: worst,
			WorstRatio:  worst / stake,
			CoverProb:   cover,
			ExpReturn:   exp / stake,
		})
	}

	// best first: highest worst-case ratio, then highest cover chance
	slices.SortStableFunc(plans, func(a, b ChainPlan) int {
		return cmp.Or(
			cmp.Compare(b.WorstRatio, a.WorstRatio),
			cmp.Compare(b.CoverProb, a.CoverProb),
		)
	})

	if top > 0 && len(plans) > top {
		plans = plans[:top]
	}
	return plans
}

func PrintPlans(plans []ChainPlan) {
	if len(plans) == 0 {
		fmt.Println("no plans found")
		return
	}
	for i, p := range plans {
		fmt.Printf("=== #%d | fights %v | stake $%.2f | worst back $%.2f (%.0f%%) | cover %.1f%% | expected %.1f%% ===\n",
			i+1, p.GroupIdx, p.Stake, p.WorstReturn, p.WorstRatio*100, p.CoverProb*100, p.ExpReturn*100)
		for _, v := range p.Bets {
			fmt.Printf("%s\n ->  odd=%-8.2f -> W=%6.2f%% -> bet=$%.2f -> price=$%.2f\n\n",
				v.Name(), v.TotalOdd, v.WinPercent(), v.Bet, v.Price)
		}
	}
}

// =====================================================
// HTML -> JSON PARSER
// =====================================================

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

var eventRe = regexp.MustCompile(`/line/[^/]+/\d+-([^/]+)/`)

// "/en/line/ufc/3072381-ufc-332/..." -> "ufc 332"
func eventName(href string) string {
	m := eventRe.FindStringSubmatch(href)
	if m == nil {
		return "unknown"
	}
	return strings.ReplaceAll(m[1], "-", " ")
}

func ParseFights(r io.Reader, bet float64) (Input, error) {
	out := Input{BetName: "unknown", Bet: bet}

	doc, err := html.Parse(r)
	if err != nil {
		return out, fmt.Errorf("bad html: %w", err)
	}

	cards := findAll(doc, "ufc-fight-card")
	if len(cards) == 0 {
		return out, fmt.Errorf("no fight cards found")
	}

	if link := findFirst(cards[0], "ufc-fight-card__link"); link != nil {
		out.BetName = eventName(attr(link, "href"))
	}

	for _, card := range cards {
		var names []string
		for _, n := range findAll(card, "ufc-fighter__name") {
			names = append(names, text(n))
		}

		// W1 / W2 only (skip X and the "+9" button)
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

		out.Groups = append(out.Groups, []Player{
			{Name: names[0], Odd: odds["W1"]},
			{Name: names[1], Odd: odds["W2"]},
		})
	}

	if len(out.Groups) == 0 {
		return out, fmt.Errorf("no fights with odds found")
	}
	return out, nil
}

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
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("saved %s | event: %s | %d fights\n", jsonPath, res.BetName, len(res.Groups))
	return nil
}

// =====================================================
// MAIN
// =====================================================

// go run . parse ufc.html d.json    # html -> json
// go run . d.json                   # all combos, saved to a txt file
// go run . d.json 50                # only the top 50 payouts
// go run . chain d.json 2 3 5       # chain analysis: 2 fights, bet 3 combos, show 5 best plans
// go run . chain d.json 3 6 5       # 3 fights, bet 6 of 8 combos

func main() {
	args := os.Args[1:]

	// go run . parse ufc.html d.json
	if len(args) >= 1 && args[0] == "parse" {
		if len(args) < 3 {
			log.Fatal("usage: go run . parse <input.html> <output.json>")
		}
		if err := ParseFile(args[1], args[2], InitBet); err != nil {
			log.Fatal(err)
		}
		return
	}

	// go run . chain d.json 2 3 5   (file, k = chain size, m = bets, top = plans)
	if len(args) >= 1 && args[0] == "chain" {
		if len(args) < 5 {
			log.Fatal("usage: go run . chain <file.json> <k> <m> <top>")
		}
		in, err := LoadInput(args[1])
		if err != nil {
			log.Fatal(err)
		}
		k, err1 := strconv.Atoi(args[2])
		m, err2 := strconv.Atoi(args[3])
		top, err3 := strconv.Atoi(args[4])
		if err1 != nil || err2 != nil || err3 != nil || k < 1 || m < 1 {
			log.Fatal("k, m and top must be whole numbers (k and m at least 1)")
		}
		if k > len(in.Groups) {
			log.Fatalf("k=%d is bigger than the number of fights (%d)", k, len(in.Groups))
		}
		fmt.Printf("event: %s | %d fights | bet $%.2f\n\n", in.BetName, len(in.Groups), in.Bet)
		PrintPlans(AnalyzeChains(in.Groups, in.Bet, k, m, top))
		return
	}

	// go run .                 -> data.json, all combos
	// go run . d.json          -> d.json, all combos
	// go run . d.json 50       -> d.json, only the top 50 payouts
	file := DefaultFile
	if len(args) >= 1 {
		file = args[0]
	}
	limit := 0
	if len(args) >= 2 {
		limit, _ = strconv.Atoi(args[1])
	}

	in, err := LoadInput(file)
	if err != nil {
		log.Fatal(err)
	}

	stacks := Stacks(in.Bet, in.Groups...)
	stacks.SortByPrice()
	if limit > 0 && limit < len(stacks) {
		stacks = stacks[:limit]
	}
	if err := stacks.PrintAndSave(in); err != nil {
		log.Fatal(err)
	}
}
