package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"log"
	"io"
	"math"
	"os"
	"slices"
	"strings"
	"time"
)

const DefaultFile = "data.json"
const InitBet float64 = 1.0 // $

type Player struct {
	Name string  `json:"name"`
	Odd  float64 `json:"odd"`
	SportOdd  float64 `json:"sport_odd"`
	Fair float64 `json:"-"`
}

type Input struct {
	BetName string    `json:"bet_name"`
	Bet    float64    `json:"bet"`
	Groups [][]Player `json:"groups"`
}

func (p Player) Implied() float64 {
	return 1 / p.Odd
}

func (p Player) Price(bet float64) float64 {
	return round2(bet * p.Odd)
}

func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

// the fair probabilities of the group add up to 100%.
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
	TotalSportOdd  float64
	WinProb   float64
	Price     float64
	Profit    float64
}

type MatchList []Versus

func NewVersus(players []Player, bet float64) Versus {
	t := Equal
	totalOdd := 1.0
	totalSportOdd := 1.0
	prob := 1.0

	for _, p := range players {
		if p.Odd != players[0].Odd {
			t = Diff
		}
		totalOdd *= p.Odd
		totalSportOdd *= p.SportOdd
		prob *= p.Fair
	}

	totalOdd = round2(totalOdd)
	price := round2(bet * totalOdd)
	
	
	return Versus{
		Opponents: players,
		Type:      t,
		Bet:       bet,
		TotalOdd:  totalOdd,
		TotalSportOdd:  totalSportOdd,
		WinProb:   prob,
		Price:     price,
		Profit:    round2(price - bet),
	}
}

func (v Versus) Name() string {
	var sb strings.Builder
	for _, p := range v.Opponents {
		sb.WriteString(p.Name)
	}
	return sb.String()
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

// 2 groups -> AC, 3 groups -> ACE ...
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

// func (h MatchList) PrintAll() {
// 	for _, v := range h {
// 
// 		parts := make([]string, len(v.Opponents))
// 		for i, p := range v.Opponents {
// 			parts[i] = fmt.Sprintf("%s [ %.2f ]", p.Name, p.Odd)
// 		}
// 
// 		fmt.Printf("%-4s %-28s [%-5s] odd=%-8.2f win=%6.2f%%  bet=$%.2f -> price=$%.2f  profit=$%.2f\n",
// 			v.Name(), strings.Join(parts, " x "), v.Type,
// 			v.TotalOdd, v.WinPercent(), v.Bet, v.Price, v.Profit)
// 	}
// }

func PrintFair(  w io.Writer ,groups ...[]Player) {
	for i, g := range groups {
		sum := 0.0
		for _, p := range g {
			sum += p.Implied()
		}
		fmt.Fprintf(w ,"\t Group %d | margin %.1f%%\n", i+1, (sum-1)*100)

		for _, p := range Normalize(g) {
			fmt.Fprintf( w ,"\t   %s [ %.2f ] -> $%.2f | implied %.1f%% | fair %.1f%%\n",
				p.Name, p.Odd, p.Price(InitBet), p.Implied()*100, p.Fair*100)
		}
	}
	fmt.Fprintf(w, "\n")
}

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
	return in, nil
}

func main() {
	file := DefaultFile
	if len(os.Args) > 1 {
		file = os.Args[1]
	}

	in, err := LoadInput(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	stacks := Stacks(InitBet, in.Groups...)
	stacks.SortByPrice()
	err = stacks.PrintAndSave(in)
	if err != nil {
		log.Fatal(err)
	}

}


func (h MatchList) PrintAndSave(in Input) error {
	filename := fmt.Sprintf("%s : _%s.txt",
		in.BetName,
		time.Now().Format("20060102_150405"),
	)

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := io.MultiWriter(os.Stdout)

	PrintFair(w ,in.Groups...)
	h.Print(w)

	return nil
}
func (h MatchList) Print(w io.Writer) {
	for _, v := range h {
		parts := make([]string, len(v.Opponents))

		for i, p := range v.Opponents {
			parts[i] = fmt.Sprintf("%s[ %.2f ]", p.Name, p.Odd)
		}

		fmt.Fprintf(w,
			"%-4s %-28s ood=%-8.2f W=%3.2f%%  bet=$%.2f -> price=$%.2f \n",
			v.Name(),
			strings.Join(parts, " + "),
			v.TotalOdd,
			v.WinPercent(),
			v.Bet,
			v.Price,
		)
	}
}
