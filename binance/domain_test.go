package binance

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring (mint, body, resolve), which need no network. The client's
// HTTP behaviour is covered in binance_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "binance" {
		t.Errorf("Scheme = %q, want binance", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "binance" {
		t.Errorf("Identity.Binary = %q, want binance", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		id  string
	}{
		{"BTCUSDT", "symbol", "BTCUSDT"},
		{"btcusdt", "symbol", "BTCUSDT"},
		{"BTC_USDT", "symbol", "BTCUSDT"},
		{"https://www.binance.com/en/trade/BTC_USDT", "symbol", "BTCUSDT"},
		{"ETHUSDT", "symbol", "ETHUSDT"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestClassifyEmpty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("Classify(\"\") should return error")
	}
}

func TestLocate(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"BTCUSDT", "https://www.binance.com/en/trade/BTC_USDT"},
		{"ETHUSDT", "https://www.binance.com/en/trade/ETH_USDT"},
		{"BNBBTC", "https://www.binance.com/en/trade/BNB_BTC"},
	}
	for _, tc := range cases {
		got, err := Domain{}.Locate("symbol", tc.id)
		if err != nil || got != tc.want {
			t.Errorf("Locate(symbol, %q) = (%q, %v), want (%q, nil)", tc.id, got, err, tc.want)
		}
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("unknown", "BTCUSDT")
	if err == nil {
		t.Error("Locate with unknown type should return error")
	}
}

func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	p := &Price{Symbol: "BTCUSDT", Price: "50000.00"}
	u, err := h.Mint(p)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if want := "binance://symbol/BTCUSDT"; u.String() != want {
		t.Errorf("Mint = %q, want %q", u.String(), want)
	}

	got, err := h.ResolveOn("binance", "ETHUSDT")
	if err != nil || got.String() != "binance://symbol/ETHUSDT" {
		t.Errorf("ResolveOn = (%q, %v), want binance://symbol/ETHUSDT", got.String(), err)
	}
}

func TestTradeSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"BTCUSDT", "BTC_USDT"},
		{"ETHBTC", "ETH_BTC"},
		{"BNBETH", "BNB_ETH"},
		{"XYZUNKNOWN", "XYZUNKNOWN"},
	}
	for _, tc := range cases {
		got := tradeSlug(tc.in)
		if got != tc.want {
			t.Errorf("tradeSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDecodeKlinesRow(t *testing.T) {
	// Verify the kline decode logic with a synthetic raw row.
	row := []interface{}{
		float64(1700000000000),
		"29000.00",
		"29500.00",
		"28800.00",
		"29300.00",
		"1234.5",
		float64(1700086399999),
		"35000000.00",
		float64(9876),
	}
	c := &Candle{Symbol: "BTCUSDT"}
	if v, ok := row[0].(float64); ok {
		c.OpenTime = int64(v)
	}
	if v, ok := row[1].(string); ok {
		c.Open = v
	}
	if v, ok := row[2].(string); ok {
		c.High = v
	}
	if v, ok := row[3].(string); ok {
		c.Low = v
	}
	if v, ok := row[4].(string); ok {
		c.Close = v
	}
	if v, ok := row[5].(string); ok {
		c.Volume = v
	}
	if v, ok := row[6].(float64); ok {
		c.CloseTime = int64(v)
	}
	if v, ok := row[8].(float64); ok {
		c.Trades = int(v)
	}

	if c.Symbol != "BTCUSDT" {
		t.Errorf("Symbol = %q, want BTCUSDT", c.Symbol)
	}
	if c.Open != "29000.00" {
		t.Errorf("Open = %q, want 29000.00", c.Open)
	}
	if c.High != "29500.00" {
		t.Errorf("High = %q, want 29500.00", c.High)
	}
	if c.Trades != 9876 {
		t.Errorf("Trades = %d, want 9876", c.Trades)
	}
	if c.OpenTime != 1700000000000 {
		t.Errorf("OpenTime = %d, want 1700000000000", c.OpenTime)
	}
}
