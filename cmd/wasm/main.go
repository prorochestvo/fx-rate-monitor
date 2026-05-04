//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"
)

type sourceInfo struct {
	Name          string `json:"name"`
	BaseCurrency  string `json:"base_currency"`
	QuoteCurrency string `json:"quote_currency"`
	Interval      string `json:"interval"`
	LastSuccess   bool   `json:"last_success"`
	LastError     string `json:"last_error"`
	LastRunAt     string `json:"last_run_at"`
}

type rateInfo struct {
	Price         float64 `json:"price"`
	BaseCurrency  string  `json:"base_currency"`
	QuoteCurrency string  `json:"quote_currency"`
	Timestamp     string  `json:"timestamp"`
}

func main() {
	doc := js.Global().Get("document")
	app := doc.Call("getElementById", "app")
	status := doc.Call("getElementById", "status")

	// Register the rate-row click handler exactly once. This Func is owned by
	// main and lives for the entire lifetime of the WASM module — releasing it
	// would invalidate inline onclick="window._loadRates(...)" callsites in the
	// rendered HTML, so we intentionally do not release it.
	loadRatesFn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) < 1 {
			return nil
		}
		go loadRates(doc, args[0].String())
		return nil
	})
	js.Global().Set("_loadRates", loadRatesFn)

	status.Set("textContent", "Fetching sources…")
	go loadSources(doc, app, status)

	select {} // keep WASM alive; without this the process exits and all callbacks become invalid
}

func loadSources(doc, app, status js.Value) {
	body, err := fetchText("/api/sources")
	if err != nil {
		status.Set("textContent", "Error: "+err.Error())
		return
	}

	var sources []sourceInfo
	if err = json.Unmarshal([]byte(body), &sources); err != nil {
		status.Set("textContent", "Parse error: "+err.Error())
		return
	}

	status.Set("textContent", fmt.Sprintf("%d source(s) loaded", len(sources)))
	renderSources(doc, app, sources)
}

func renderSources(_, app js.Value, sources []sourceInfo) {
	html := `<h2>Sources</h2><table><tr><th>Name</th><th>Pair</th><th>Interval</th><th>Last Run</th><th>Status</th></tr>`
	for _, s := range sources {
		cell := "⏳ Never run"
		if s.LastRunAt != "" {
			if s.LastSuccess {
				cell = "✅ OK"
			} else {
				cell = "❌ " + s.LastError
			}
		}
		html += fmt.Sprintf(
			`<tr><td><a onclick="window._loadRates('%s')" href="#">%s</a></td><td>%s/%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
			s.Name, s.Name, s.BaseCurrency, s.QuoteCurrency, s.Interval, s.LastRunAt, cell,
		)
	}
	html += `</table><div id="rates"></div>`
	app.Set("innerHTML", html)
}

func loadRates(doc js.Value, name string) {
	body, err := fetchText("/api/sources/" + name + "/rates?limit=50")
	if err != nil {
		return
	}

	var rates []rateInfo
	if err = json.Unmarshal([]byte(body), &rates); err != nil {
		return
	}

	html := fmt.Sprintf(`<h2>Rate History: %s</h2><table><tr><th>Price</th><th>Pair</th><th>Timestamp</th></tr>`, name)
	for _, r := range rates {
		html += fmt.Sprintf(`<tr><td>%.4f</td><td>%s/%s</td><td>%s</td></tr>`,
			r.Price, r.BaseCurrency, r.QuoteCurrency, r.Timestamp)
	}
	html += `</table>`

	d := doc.Call("getElementById", "rates")
	if !d.IsNull() && !d.IsUndefined() {
		d.Set("innerHTML", html)
	}
}

// fetchText calls the browser's fetch() API and returns the response body as a string.
// Must be called from a goroutine, never from the main goroutine (which holds the JS event loop).
//
// Each FuncOf must be released after the promise chain settles, otherwise every API
// call leaks four entries from the runtime's funcs table.
func fetchText(url string) (string, error) {
	type result struct {
		val string
		err error
	}
	ch := make(chan result, 1)

	var thenFn, textOK, textErr, fetchErr js.Func
	thenFn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		args[0].Call("text").Call("then", textOK, textErr)
		return nil
	})
	textOK = js.FuncOf(func(_ js.Value, inner []js.Value) any {
		ch <- result{val: inner[0].String()}
		return nil
	})
	textErr = js.FuncOf(func(_ js.Value, inner []js.Value) any {
		ch <- result{err: fmt.Errorf("body: %s", inner[0].String())}
		return nil
	})
	fetchErr = js.FuncOf(func(_ js.Value, args []js.Value) any {
		ch <- result{err: fmt.Errorf("fetch: %s", args[0].String())}
		return nil
	})
	defer thenFn.Release()
	defer textOK.Release()
	defer textErr.Release()
	defer fetchErr.Release()

	js.Global().Call("fetch", url).Call("then", thenFn, fetchErr)

	r := <-ch
	return r.val, r.err
}
