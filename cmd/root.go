package cmd

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/dev-shimada/csv-http-request-tool/internal/csv"
	internalhttp "github.com/dev-shimada/csv-http-request-tool/internal/http"
	"github.com/dev-shimada/csv-http-request-tool/internal/request"
	"github.com/dev-shimada/csv-http-request-tool/internal/worker"
	"github.com/spf13/cobra"
)

var (
	csvPath        string
	urlTemplate    string
	headerTemplate string
	bodyTemplate   string
	method         string
	parallel       int
	timeout        int
	rate           int
	verbose        bool
	dryRun         bool
	humanReadable  bool
)

var rootCmd = &cobra.Command{
	Use:   "run",
	Short: "A CLI tool to send requests based on a CSV file.",
	Run: func(cmd *cobra.Command, args []string) {
		// ログレベルの設定
		var programLevel = new(slog.LevelVar)
		switch {
		case verbose:
			programLevel.Set(slog.LevelDebug)
		default:
			programLevel.Set(slog.LevelWarn)
		}
		handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: programLevel})
		logger := slog.New(handler)
		slog.SetDefault(logger)
		// logをslog経由で出力
		log.SetOutput(slog.NewLogLogger(handler, slog.LevelInfo).Writer())

		if csvPath == "" || urlTemplate == "" {
			cmd.Help()
			os.Exit(1)
		}

		file, err := os.Open(csvPath)
		if err != nil {
			slog.Error(fmt.Sprintf("failed to open csv file: %v\n", err))
			os.Exit(1)
		}
		defer file.Close()

		records, err := csv.Read(file)
		if err != nil {
			slog.Error(fmt.Sprintf("failed to read csv: %v\n", err))
			os.Exit(1)
		}

		csvData, err := csv.NewCSV(records)
		if err != nil {
			slog.Error(fmt.Sprintf("failed to parse csv: %v\n", err))
			os.Exit(1)
		}

		factory := request.NewFactory(method, urlTemplate, headerTemplate, bodyTemplate)
		reqs := make(chan *http.Request, len(csvData.Body))
		for _, row := range csvData.Body {
			request, err := factory.Build(csvData.Header, row)
			if err != nil {
				slog.Warn(fmt.Sprintf("failed to build request: %v\n", err))
				continue
			}
			reqs <- request
		}
		close(reqs)

		client := internalhttp.NewClient(time.Duration(timeout) * time.Second)
		pool := worker.NewPool(client, parallel, rate, dryRun, humanReadable)
		ctx := context.Background()
		pool.Run(ctx, reqs)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		slog.Error(fmt.Sprintf("command execution failed: %v\n", err))
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&csvPath, "csv", "c", "", "Path to CSV file (required)")
	rootCmd.Flags().StringVarP(&urlTemplate, "url", "u", "", "URL template (required)")
	rootCmd.Flags().StringVar(&headerTemplate, "header", "", "Header template")
	rootCmd.Flags().StringVarP(&bodyTemplate, "body", "b", "", "Body template")
	rootCmd.Flags().StringVarP(&method, "method", "m", "GET", "HTTP method")
	rootCmd.Flags().IntVarP(&parallel, "parallel", "p", 1, "Number of parallel requests")
	rootCmd.Flags().IntVarP(&timeout, "timeout", "t", 30, "Request timeout in seconds")
	rootCmd.Flags().IntVarP(&rate, "rate", "r", 0, "Rate limit in requests per second")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Dry run mode (no requests sent)")
	rootCmd.Flags().BoolVarP(&humanReadable, "human-readable", "H", false, "Output in human-readable format")

	rootCmd.MarkFlagRequired("csv")
	rootCmd.MarkFlagRequired("url")
}
