package worker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/olekukonko/tablewriter"
)

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Pool struct {
	client        Doer
	numWorker     int
	rate          int
	dryRun        bool
	humanReadable bool
}

func NewPool(client Doer, numWorker, rate int, dryRun, humanReadable bool) *Pool {
	return &Pool{
		client:        client,
		numWorker:     numWorker,
		rate:          rate,
		dryRun:        dryRun,
		humanReadable: humanReadable,
	}
}

func (p *Pool) Run(ctx context.Context, reqs <-chan *http.Request) {
	ctx1, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	var ticker *time.Ticker
	if p.rate > 0 {
		ticker = time.NewTicker(time.Second / time.Duration(p.rate))
	}
	type out struct {
		req *http.Request
		res *http.Response
		err error
	}
	co := make(chan out, len(reqs))

	for i := 0; i < p.numWorker; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ctx1.Done():
				slog.Info("worker stopped due to context cancellation")
				return
			default:
			}
			for req := range reqs {
				if ticker != nil {
					<-ticker.C
				}
				if p.dryRun {
					co <- out{req: req, res: nil, err: nil}
					continue
				}
				resp, err := p.client.Do(req)
				if err != nil {
					co <- out{req: req, res: nil, err: err}
					continue
				}
				if resp != nil && resp.Body != nil {
					err := resp.Body.Close()
					if err != nil {
						co <- out{req: req, res: nil, err: fmt.Errorf("failed to close response body: %w", err)}
						continue
					}
				}
				co <- out{req: req, res: resp, err: err}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(co)
	}()

	outputs := make([]out, 0, cap(co))
	for o := range co {
		outputs = append(outputs, o)
	}

	if p.humanReadable {
		tableString := &strings.Builder{}
		table := tablewriter.NewWriter(tableString)
		table.Header([]string{"URL", "Status", "Body", "Error"})
		for _, out := range outputs {
			var status, body, errMsg string
			if out.err != nil {
				errMsg = out.err.Error()
			}
			if out.res != nil {
				status = fmt.Sprintf("%d", out.res.StatusCode)
				if out.res.Body != nil {
					// Body is already closed in worker, so this will be empty
					bodyBytes, _ := io.ReadAll(out.res.Body)
					body = string(bodyBytes)
				}
			}
			_ = table.Append([]string{
				out.req.URL.String(),
				status,
				body,
				errMsg,
			})
		}
		if err := table.Render(); err != nil {
			slog.Error(fmt.Sprintf("failed to render table: %v", err))
			return
		}
		fmt.Println(tableString.String())
	} else if p.dryRun {
		for _, out := range outputs {
			slog.Info(fmt.Sprintf("Dry run for %s", out.req.URL))
		}
	} else {
		for _, out := range outputs {
			if out.err != nil {
				slog.Error(fmt.Sprintf("Error for %s: %v", out.req.URL, out.err))
			} else if out.res == nil {
				slog.Error(fmt.Sprintf("No response for %s", out.req.URL))
			} else if out.res.StatusCode >= 200 && out.res.StatusCode < 400 {
				slog.Info(fmt.Sprintf("Success for %s: %d", out.req.URL, out.res.StatusCode))
			} else {
				slog.Warn(fmt.Sprintf("Non-OK status for %s: %d", out.req.URL, out.res.StatusCode))
			}
		}
	}
	if ticker != nil {
		ticker.Stop()
	}
}
