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
					slog.Info(fmt.Sprintf("dry run: %s\n", req.URL))
					continue
				}
				resp, err := p.client.Do(req)
				if err != nil {
					slog.Error(fmt.Sprintf("request error: %v\n", err))
					continue
				}
				if resp != nil && resp.Body != nil {
					resp.Body.Close()
				}
				co <- out{req: req, res: resp, err: err}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(co)
	}()

	if p.humanReadable {
		tableString := &strings.Builder{}
		table := tablewriter.NewWriter(tableString)
		table.Header([]string{"URL", "Status", "Body", "Error"})
		for out := range co {
			if out.err != nil {
				slog.Error(fmt.Sprintf("error processing request: %v", out.err))
				continue
			}
			body := ""
			if out.res.Body != nil {
				bodyBytes, _ := io.ReadAll(out.res.Body)
				body = string(bodyBytes)
			}
			table.Append([]string{
				out.req.URL.String(),
				fmt.Sprintf("%d", out.res.StatusCode),
				body,
				fmt.Sprintf("%v", out.err),
			})
		}
		table.Render()
		fmt.Println(tableString.String())
	}
	if ticker != nil {
		ticker.Stop()
	}
}
