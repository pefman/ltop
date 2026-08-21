package format

import (
	"fmt"
	"math"
	"strings"
)

// Currency is an electricity-cost unit the dashboard can cycle with c / C.
type Currency struct {
	Code   string // ISO 4217
	Symbol string // prefix; empty means the amount is followed by Code
}

var (
	EUR = Currency{Code: "EUR", Symbol: "€"}
	USD = Currency{Code: "USD", Symbol: "$"}
	GBP = Currency{Code: "GBP", Symbol: "£"}
	SEK = Currency{Code: "SEK"}
	NOK = Currency{Code: "NOK"}
	DKK = Currency{Code: "DKK"}
	CHF = Currency{Code: "CHF"}
	PLN = Currency{Code: "PLN"}
	CAD = Currency{Code: "CAD", Symbol: "C$"}
	AUD = Currency{Code: "AUD", Symbol: "A$"}
)

// Currencies is the c / C cycle: widely used household tariffs, plus SEK.
var Currencies = []Currency{EUR, USD, GBP, SEK, NOK, DKK, CHF, PLN, CAD, AUD}

// CurrencyIndex returns the cycle position for an ISO code, or 0 (EUR) if
// the code is empty or unknown.
func CurrencyIndex(code string) int {
	for i, c := range Currencies {
		if strings.EqualFold(c.Code, code) {
			return i
		}
	}
	return 0
}

// Format renders a running electricity cost in this currency.
func (c Currency) Format(v float64) string {
	n := amount(v)
	if c.Symbol != "" {
		return c.Symbol + n
	}
	return n + " " + c.Code
}

// Tariff renders a per-kWh price, as shown in the footer.
func (c Currency) Tariff(perKWh float64) string {
	if perKWh < 0 || math.IsNaN(perKWh) {
		perKWh = 0
	}
	if c.Symbol != "" {
		return fmt.Sprintf("%s%.2f/kWh", c.Symbol, perKWh)
	}
	return fmt.Sprintf("%.2f %s/kWh", perKWh, c.Code)
}

func amount(v float64) string {
	if v < 0 || math.IsNaN(v) {
		v = 0
	}
	if v < 1 {
		return fmt.Sprintf("%.4f", v)
	}
	return fmt.Sprintf("%.2f", v)
}
