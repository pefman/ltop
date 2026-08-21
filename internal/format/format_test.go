package format

import (
	"math"
	"testing"
)

func TestEuros(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "€0.0000"},
		{0.0002, "€0.0002"},
		{0.02, "€0.0200"},
		{0.9999, "€0.9999"},
		{1, "€1.00"},
		{1.5, "€1.50"},
		{12.345, "€12.35"},
	}
	for _, tc := range cases {
		if got := Euros(tc.in); got != tc.want {
			t.Errorf("Euros(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMoneyFormatsCurrencies(t *testing.T) {
	cases := []struct {
		cur  Currency
		in   float64
		want string
	}{
		{EUR, 0.02, "€0.0200"},
		{USD, 0.02, "$0.0200"},
		{GBP, 1.5, "£1.50"},
		{SEK, 0.02, "0.0200 SEK"},
		{SEK, 1.5, "1.50 SEK"},
		{NOK, 0.02, "0.0200 NOK"},
	}
	for _, tc := range cases {
		if got := tc.cur.Format(tc.in); got != tc.want {
			t.Errorf("%s.Format(%v) = %q, want %q", tc.cur.Code, tc.in, got, tc.want)
		}
	}
}

func TestCurrenciesCycleIncludesCommonAndSEK(t *testing.T) {
	want := []string{"EUR", "USD", "GBP", "SEK", "NOK", "DKK", "CHF", "PLN", "CAD", "AUD"}
	if len(Currencies) != len(want) {
		t.Fatalf("Currencies has %d entries, want %d", len(Currencies), len(want))
	}
	for i, code := range want {
		if Currencies[i].Code != code {
			t.Errorf("Currencies[%d] = %s, want %s", i, Currencies[i].Code, code)
		}
	}
}

func TestCurrencyIndex(t *testing.T) {
	if got := CurrencyIndex("SEK"); Currencies[got].Code != "SEK" {
		t.Errorf("CurrencyIndex(SEK) = %d (%s)", got, Currencies[got].Code)
	}
	if got := CurrencyIndex("sek"); Currencies[got].Code != "SEK" {
		t.Errorf("CurrencyIndex(sek) = %d, want case-insensitive SEK", got)
	}
	if got := CurrencyIndex(""); Currencies[got].Code != "EUR" {
		t.Errorf("CurrencyIndex(empty) = %s, want EUR", Currencies[got].Code)
	}
	if got := CurrencyIndex("XXX"); Currencies[got].Code != "EUR" {
		t.Errorf("CurrencyIndex(unknown) = %s, want EUR", Currencies[got].Code)
	}
}

func TestTariffRendersPerKWh(t *testing.T) {
	if got := EUR.Tariff(0.20); got != "€0.20/kWh" {
		t.Errorf("EUR.Tariff = %q", got)
	}
	if got := SEK.Tariff(1.50); got != "1.50 SEK/kWh" {
		t.Errorf("SEK.Tariff = %q", got)
	}
}

func TestEnergyCostEUR(t *testing.T) {
	cases := []struct {
		wh, price, want float64
	}{
		{100, 0.20, 0.02},  // 0.1 kWh × €0.20
		{1000, 0.30, 0.30}, // 1 kWh × €0.30
		{0, 0.20, 0},
		{100, 0, 0},
	}
	for _, tc := range cases {
		got := EnergyCostEUR(tc.wh, tc.price)
		if math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("EnergyCostEUR(%v, %v) = %v, want %v", tc.wh, tc.price, got, tc.want)
		}
	}
}
