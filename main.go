package main
import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
)

const InitBet float64 = 1.0 // $
type Player struct {
	Name string
	Odd  float64 // example: 1.1, 2.3, 4.5
	Fair float64 // win probability with the bookmaker margin removed
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

// Normalize one group (= one event, like A vs B):
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
	Bet       float64 // money put in
	TotalOdd  float64 // all odds multiplied (rounded to 2 decimals)
	WinProb   float64 // fair chance that ALL legs win
	Price     float64 // total return, stake included (1xBet "Possible win")
	Profit    float64 // Price - Bet
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

// One player from EACH group: 2 groups -> AC, 3 groups -> ACE ...
func Stacks(bet float64, groups ...[]Player) MatchList {
	if len(groups) == 0 {
		return nil
	}

	// remove the margin inside each group first
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

func (h MatchList) PrintAll() {
	for _, v := range h {


		parts := make([]string, len(v.Opponents))
		for i, p := range v.Opponents {
			parts[i] = fmt.Sprintf("%s [ %.2f ]", p.Name, p.Odd)
		}
		fmt.Printf("%-4s %-28s [%-5s] odd=%-8.2f win=%6.2f%%  bet=$%.2f -> price=$%.2f  profit=$%.2f\n",
			v.Name(), strings.Join(parts, " x "), v.Type,
			v.TotalOdd, v.WinPercent(), v.Bet, v.Price, v.Profit)
	}
}

func main() {
	A := Player{Name: "A", Odd: 5.6}
	B := Player{Name: "B", Odd: 1.142}
	C := Player{Name: "C", Odd: 1.47}
	D := Player{Name: "D", Odd: 2.725}
	// E := Player{Name: "E", Odd: 2.0}
	// F := Player{Name: "F", Odd: 3.0}

	group1 := []Player{A, B}
	group2 := []Player{C, D}
//	group3 := []Player{E, F}
	PrintFair(group1,group2)
	stacks := Stacks(InitBet, group1, group2)

	stacks.SortByPrice()
	stacks.PrintAll()

}


func PrintFair(groups ...[]Player) {
	for i, g := range groups {
		sum := 0.0
		for _, p := range g {
			sum += p.Implied()
		}
		fmt.Printf("\t Group %d | margin %.1f%%\n", i+1, (sum-1)*100)

		for _, p := range Normalize(g) {
			fmt.Printf("\t   %s [ %.2f ] -> $%.2f | implied %.1f%% | fair %.1f%%\n",
				p.Name, p.Odd, p.Price(InitBet), p.Implied()*100, p.Fair*100)
		}
		fmt.Println()
	}
}
