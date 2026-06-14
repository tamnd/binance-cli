// Package binance is the library behind the binance command line:
// the HTTP client, request shaping, and the typed data models for Binance public market data.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public API throws under load.
package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// DefaultUserAgent identifies the client to Binance.
const DefaultUserAgent = "binance-cli/dev (+https://github.com/tamnd/binance-cli)"

// Host is the API host this client talks to.
const Host = "api.binance.com"

// BaseURL is the root every request is built from.
const BaseURL = "https://" + Host

// Client talks to the Binance public API over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults: a 30s timeout, a 200ms
// minimum gap between requests, and five retries on transient errors.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: DefaultUserAgent,
		Rate:      200 * time.Millisecond,
		Retries:   5,
	}
}

// Get fetches url and returns the response body. It paces and retries according
// to the client's settings. The caller owns nothing extra; the body is read
// fully and closed here.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// --- data models ---

// Price is the current price for a symbol.
type Price struct {
	Symbol string `kit:"id" json:"symbol"`
	Price  string `json:"price"`
}

// Ticker holds 24-hour rolling-window statistics for a symbol.
type Ticker struct {
	Symbol         string `kit:"id" json:"symbol"`
	PriceChange    string `json:"price_change"`
	PriceChangePct string `json:"price_change_pct"`
	LastPrice      string `json:"last_price"`
	HighPrice      string `json:"high_price"`
	LowPrice       string `json:"low_price"`
	Volume         string `json:"volume"`
	QuoteVolume    string `json:"quote_volume"`
}

// Candle is one OHLCV candlestick bar.
type Candle struct {
	Symbol    string `kit:"id" json:"symbol"`
	OpenTime  int64  `json:"open_time"`
	Open      string `json:"open"`
	High      string `json:"high"`
	Low       string `json:"low"`
	Close     string `json:"close"`
	Volume    string `json:"volume"`
	CloseTime int64  `json:"close_time"`
	Trades    int    `json:"trades"`
}

// Symbol describes a trading pair listed on Binance.
type Symbol struct {
	Symbol     string `kit:"id" json:"symbol"`
	BaseAsset  string `json:"base_asset"`
	QuoteAsset string `json:"quote_asset"`
	Status     string `json:"status"`
}

// --- wire types (direct Binance JSON shapes) ---

type wireTicker struct {
	Symbol             string `json:"symbol"`
	PriceChange        string `json:"priceChange"`
	PriceChangePercent string `json:"priceChangePercent"`
	WeightedAvgPrice   string `json:"weightedAvgPrice"`
	LastPrice          string `json:"lastPrice"`
	HighPrice          string `json:"highPrice"`
	LowPrice           string `json:"lowPrice"`
	Volume             string `json:"volume"`
	QuoteVolume        string `json:"quoteVolume"`
}

func (w wireTicker) toTicker() *Ticker {
	return &Ticker{
		Symbol:         w.Symbol,
		PriceChange:    w.PriceChange,
		PriceChangePct: w.PriceChangePercent,
		LastPrice:      w.LastPrice,
		HighPrice:      w.HighPrice,
		LowPrice:       w.LowPrice,
		Volume:         w.Volume,
		QuoteVolume:    w.QuoteVolume,
	}
}

type exchangeInfoResponse struct {
	Symbols []struct {
		Symbol     string `json:"symbol"`
		BaseAsset  string `json:"baseAsset"`
		QuoteAsset string `json:"quoteAsset"`
		Status     string `json:"status"`
	} `json:"symbols"`
}

// --- API methods ---

// GetPrice fetches the current price for a single symbol.
func (c *Client) GetPrice(ctx context.Context, symbol string) (*Price, error) {
	u := BaseURL + "/api/v3/ticker/price?symbol=" + url.QueryEscape(symbol)
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var p Price
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("parse price: %w", err)
	}
	return &p, nil
}

// GetAllPrices fetches current prices for all symbols.
func (c *Client) GetAllPrices(ctx context.Context) ([]*Price, error) {
	body, err := c.Get(ctx, BaseURL+"/api/v3/ticker/price")
	if err != nil {
		return nil, err
	}
	var prices []*Price
	if err := json.Unmarshal(body, &prices); err != nil {
		return nil, fmt.Errorf("parse prices: %w", err)
	}
	return prices, nil
}

// GetTicker fetches 24-hour rolling stats for a single symbol.
func (c *Client) GetTicker(ctx context.Context, symbol string) (*Ticker, error) {
	u := BaseURL + "/api/v3/ticker/24hr?symbol=" + url.QueryEscape(symbol)
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var w wireTicker
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("parse ticker: %w", err)
	}
	return w.toTicker(), nil
}

// GetAllTickers fetches 24-hour rolling stats for every symbol.
func (c *Client) GetAllTickers(ctx context.Context) ([]*Ticker, error) {
	body, err := c.Get(ctx, BaseURL+"/api/v3/ticker/24hr")
	if err != nil {
		return nil, err
	}
	var ws []wireTicker
	if err := json.Unmarshal(body, &ws); err != nil {
		return nil, fmt.Errorf("parse tickers: %w", err)
	}
	out := make([]*Ticker, len(ws))
	for i, w := range ws {
		out[i] = w.toTicker()
	}
	return out, nil
}

// GetKlines fetches candlestick data for a symbol.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]*Candle, error) {
	u := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=%d",
		BaseURL, url.QueryEscape(symbol), url.QueryEscape(interval), limit)
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var raw [][]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse klines: %w", err)
	}
	out := make([]*Candle, 0, len(raw))
	for _, row := range raw {
		if len(row) < 9 {
			continue
		}
		c := &Candle{Symbol: symbol}
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
		out = append(out, c)
	}
	return out, nil
}

// GetTopGainers fetches all 24hr tickers and returns the top N by price change percentage.
func (c *Client) GetTopGainers(ctx context.Context, limit int) ([]*Ticker, error) {
	tickers, err := c.GetAllTickers(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(tickers, func(i, j int) bool {
		pi, _ := strconv.ParseFloat(tickers[i].PriceChangePct, 64)
		pj, _ := strconv.ParseFloat(tickers[j].PriceChangePct, 64)
		return pi > pj
	})
	if limit > 0 && len(tickers) > limit {
		tickers = tickers[:limit]
	}
	return tickers, nil
}

// GetSymbols fetches all trading pairs from exchangeInfo.
func (c *Client) GetSymbols(ctx context.Context, limit int) ([]*Symbol, error) {
	body, err := c.Get(ctx, BaseURL+"/api/v3/exchangeInfo")
	if err != nil {
		return nil, err
	}
	var info exchangeInfoResponse
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parse exchangeInfo: %w", err)
	}
	out := make([]*Symbol, 0, len(info.Symbols))
	for _, s := range info.Symbols {
		out = append(out, &Symbol{
			Symbol:     s.Symbol,
			BaseAsset:  s.BaseAsset,
			QuoteAsset: s.QuoteAsset,
			Status:     s.Status,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}
