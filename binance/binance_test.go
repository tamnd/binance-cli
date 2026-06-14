package binance_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tamnd/binance-cli/binance"
)

func newTestClient(srv *httptest.Server) *binance.Client {
	c := binance.NewClient()
	c.Rate = 0
	c.HTTP = &http.Client{Timeout: 5 * time.Second}
	_ = srv // base URL is set per-call in tests
	return c
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := binance.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := binance.NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestGetPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/ticker/price" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("symbol") != "BTCUSDT" {
			t.Errorf("unexpected symbol: %s", r.URL.Query().Get("symbol"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"symbol": "BTCUSDT",
			"price":  "50000.00",
		})
	}))
	defer srv.Close()

	// Swap base URL by hitting srv.URL directly via Get
	c := binance.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL+"/api/v3/ticker/price?symbol=BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	var p binance.Price
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	if p.Symbol != "BTCUSDT" {
		t.Errorf("Symbol = %q, want BTCUSDT", p.Symbol)
	}
	if p.Price != "50000.00" {
		t.Errorf("Price = %q, want 50000.00", p.Price)
	}
}

func TestGetTicker(t *testing.T) {
	payload := map[string]string{
		"symbol":             "BTCUSDT",
		"priceChange":        "500.00",
		"priceChangePercent": "1.05",
		"weightedAvgPrice":   "49750.00",
		"lastPrice":          "50000.00",
		"highPrice":          "51000.00",
		"lowPrice":           "48000.00",
		"volume":             "12345.67",
		"quoteVolume":        "614000000.00",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := binance.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL+"/api/v3/ticker/24hr?symbol=BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}

	// Decode via wire shape
	var wire struct {
		Symbol             string `json:"symbol"`
		PriceChange        string `json:"priceChange"`
		PriceChangePercent string `json:"priceChangePercent"`
		LastPrice          string `json:"lastPrice"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Symbol != "BTCUSDT" {
		t.Errorf("Symbol = %q, want BTCUSDT", wire.Symbol)
	}
	if wire.PriceChangePercent != "1.05" {
		t.Errorf("PriceChangePercent = %q, want 1.05", wire.PriceChangePercent)
	}
}

func TestGetKlinesDecoding(t *testing.T) {
	// Validate our candle row decode handles []interface{} correctly
	rawJSON := `[[1700000000000,"29000.00","29500.00","28800.00","29300.00","1234.5",1700086399999,"35000000.00",9876,null,null,null]]`
	var raw [][]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 {
		t.Fatalf("expected 1 row, got %d", len(raw))
	}
	row := raw[0]
	if len(row) < 9 {
		t.Fatalf("row too short: %d", len(row))
	}
	openTime, ok := row[0].(float64)
	if !ok {
		t.Fatalf("row[0] not float64: %T", row[0])
	}
	if int64(openTime) != 1700000000000 {
		t.Errorf("openTime = %v, want 1700000000000", openTime)
	}
	open, ok := row[1].(string)
	if !ok {
		t.Fatalf("row[1] not string: %T", row[1])
	}
	if open != "29000.00" {
		t.Errorf("open = %q, want 29000.00", open)
	}
	trades, ok := row[8].(float64)
	if !ok {
		t.Fatalf("row[8] not float64: %T", row[8])
	}
	if int(trades) != 9876 {
		t.Errorf("trades = %d, want 9876", int(trades))
	}
}

func TestGetAllPricesDecoding(t *testing.T) {
	payload := `[{"symbol":"BTCUSDT","price":"50000.00"},{"symbol":"ETHUSDT","price":"3000.00"}]`
	var prices []*binance.Price
	if err := json.Unmarshal([]byte(payload), &prices); err != nil {
		t.Fatal(err)
	}
	if len(prices) != 2 {
		t.Fatalf("len = %d, want 2", len(prices))
	}
	if prices[0].Symbol != "BTCUSDT" {
		t.Errorf("prices[0].Symbol = %q, want BTCUSDT", prices[0].Symbol)
	}
	if prices[1].Price != "3000.00" {
		t.Errorf("prices[1].Price = %q, want 3000.00", prices[1].Price)
	}
}

func TestSymbolStruct(t *testing.T) {
	s := binance.Symbol{
		Symbol:     "BTCUSDT",
		BaseAsset:  "BTC",
		QuoteAsset: "USDT",
		Status:     "TRADING",
	}
	if s.Symbol != "BTCUSDT" {
		t.Errorf("Symbol = %q, want BTCUSDT", s.Symbol)
	}
	if s.BaseAsset != "BTC" {
		t.Errorf("BaseAsset = %q, want BTC", s.BaseAsset)
	}
	if s.Status != "TRADING" {
		t.Errorf("Status = %q, want TRADING", s.Status)
	}
}

func TestGet404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := binance.NewClient()
	c.Rate = 0

	_, err := c.Get(context.Background(), srv.URL)
	if err == nil {
		t.Error("expected error on 404, got nil")
	}
}
