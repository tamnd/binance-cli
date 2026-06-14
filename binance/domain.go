package binance

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes Binance public market data as a kit Domain: a driver that a
// multi-domain host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/binance-cli/binance"
//
// The init below registers it; the host then dereferences binance:// URIs by
// routing to the operations Register installs. The same Domain also builds the
// standalone binance binary (see cli.NewApp), so the binary and a host share
// one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the Binance driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "binance",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "binance",
			Short:  "A command line for Binance public market data.",
			Long: `A command line for Binance public market data.

binance reads public Binance API data over plain HTTPS, shapes it into
clean records, and prints output that pipes into the rest of your tools.
No API key required.`,
			Site: "www.binance.com",
			Repo: "https://github.com/tamnd/binance-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name: "price", Group: "market", Single: true,
		Summary: "Current price for a symbol", URIType: "symbol", Resolver: true,
		Args: []kit.Arg{{Name: "symbol", Help: "symbol e.g. BTCUSDT"}},
	}, getPrice)

	kit.Handle(app, kit.OpMeta{
		Name: "ticker", Group: "market", Single: true,
		Summary: "24-hour rolling stats for a symbol", URIType: "symbol",
		Args: []kit.Arg{{Name: "symbol", Help: "symbol e.g. BTCUSDT"}},
	}, getTicker)

	kit.Handle(app, kit.OpMeta{
		Name: "klines", Group: "market", List: true,
		Summary: "Candlestick bars for a symbol", URIType: "symbol",
		Args: []kit.Arg{{Name: "symbol", Help: "symbol e.g. BTCUSDT"}},
	}, getKlines)

	kit.Handle(app, kit.OpMeta{
		Name: "gainers", Group: "market", List: true,
		Summary: "Top symbols by 24-hour price change", URIType: "symbol",
	}, getGainers)

	kit.Handle(app, kit.OpMeta{
		Name: "symbols", Group: "market", List: true,
		Summary: "All trading pairs listed on Binance", URIType: "symbol",
	}, getSymbols)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type priceInput struct {
	Symbol string  `kit:"arg" help:"symbol e.g. BTCUSDT"`
	Client *Client `kit:"inject"`
}

type tickerInput struct {
	Symbol string  `kit:"arg" help:"symbol e.g. BTCUSDT"`
	Client *Client `kit:"inject"`
}

type klinesInput struct {
	Symbol   string  `kit:"arg" help:"symbol e.g. BTCUSDT"`
	Interval string  `kit:"flag" help:"1m|5m|1h|1d" default:"1d"`
	Limit    int     `kit:"flag,inherit" help:"max candles" default:"10"`
	Client   *Client `kit:"inject"`
}

type gainersInput struct {
	Limit  int     `kit:"flag,inherit" help:"top N" default:"10"`
	Client *Client `kit:"inject"`
}

type symbolsInput struct {
	Limit  int     `kit:"flag,inherit"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func getPrice(ctx context.Context, in priceInput, emit func(*Price) error) error {
	p, err := in.Client.GetPrice(ctx, strings.ToUpper(strings.TrimSpace(in.Symbol)))
	if err != nil {
		return err
	}
	return emit(p)
}

func getTicker(ctx context.Context, in tickerInput, emit func(*Ticker) error) error {
	t, err := in.Client.GetTicker(ctx, strings.ToUpper(strings.TrimSpace(in.Symbol)))
	if err != nil {
		return err
	}
	return emit(t)
}

func getKlines(ctx context.Context, in klinesInput, emit func(*Candle) error) error {
	interval := in.Interval
	if interval == "" {
		interval = "1d"
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	candles, err := in.Client.GetKlines(ctx, strings.ToUpper(strings.TrimSpace(in.Symbol)), interval, limit)
	if err != nil {
		return err
	}
	for _, c := range candles {
		if err := emit(c); err != nil {
			return err
		}
	}
	return nil
}

func getGainers(ctx context.Context, in gainersInput, emit func(*Ticker) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	tickers, err := in.Client.GetTopGainers(ctx, limit)
	if err != nil {
		return err
	}
	for _, t := range tickers {
		if err := emit(t); err != nil {
			return err
		}
	}
	return nil
}

func getSymbols(ctx context.Context, in symbolsInput, emit func(*Symbol) error) error {
	syms, err := in.Client.GetSymbols(ctx, in.Limit)
	if err != nil {
		return err
	}
	for _, s := range syms {
		if err := emit(s); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver: URI-native string functions, pure and network-free ---

// Classify turns any accepted input into the canonical (type, id).
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty binance reference")
	}
	// Strip URL prefix if given (e.g. https://www.binance.com/en/trade/BTC_USDT)
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		// Extract symbol from URL path: /en/trade/BTC_USDT → BTCUSDT
		parts := strings.Split(strings.TrimRight(input, "/"), "/")
		last := parts[len(parts)-1]
		id = strings.ToUpper(strings.ReplaceAll(last, "_", ""))
	} else {
		id = strings.ToUpper(strings.ReplaceAll(input, "_", ""))
	}
	if id == "" {
		return "", "", errs.Usage("unrecognized binance reference: %q", input)
	}
	return "symbol", id, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	if uriType != "symbol" {
		return "", errs.Usage("binance has no resource type %q", uriType)
	}
	// Convert BTCUSDT → BTC_USDT for the trade URL (best effort: known quote assets)
	return "https://www.binance.com/en/trade/" + tradeSlug(id), nil
}

// tradeSlug converts a symbol like BTCUSDT into BTC_USDT for use in trade URLs.
// It tries known quote assets in order of length (longest first).
func tradeSlug(symbol string) string {
	quotes := []string{"USDT", "BUSD", "USDC", "BTC", "ETH", "BNB", "DAI", "TRX", "XRP"}
	for _, q := range quotes {
		if strings.HasSuffix(symbol, q) {
			base := symbol[:len(symbol)-len(q)]
			if base != "" {
				return base + "_" + q
			}
		}
	}
	return symbol
}

// mapErr converts a library error into the kit error kind that carries the right
// exit code.
func mapErr(err error) error {
	return err
}
