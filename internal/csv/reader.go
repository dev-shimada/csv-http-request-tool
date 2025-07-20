package csv

import (
	"encoding/csv"
	"io"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

func Read(r io.Reader) ([][]string, error) {
	// Use BOMOverride to handle UTF-8 with BOM
	fallback := unicode.UTF8.NewDecoder()
	data := transform.NewReader(r, unicode.BOMOverride(fallback))
	reader := csv.NewReader(data)
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	return records, nil
}
