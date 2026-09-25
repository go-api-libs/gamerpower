package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/MarkRosemaker/openapi"
	enrich "github.com/MarkRosemaker/openapi-enrich"
	"github.com/MarkRosemaker/openapi-enrich/cassette"
	"github.com/MarkRosemaker/openapi-enrich/recorder"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "openapi-enrich: %v\n", err)
		flag.Usage()
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	var (
		specPath, iaPath, auth string
		trimExamples           int
	)

	flag.StringVar(&specPath, "spec", "api/openapi.json", "path to OpenAPI spec file")
	flag.StringVar(&iaPath, "ia", "api/interactions.json", "path to interactions file")
	flag.StringVar(&auth, "auth", "", "authorization header")
	flag.IntVar(&trimExamples, "trim-examples", 0,
		"cap array length in recorded response bodies to this many representative elements (0 leaves them as recorded)")
	flag.Parse()

	doc, err := openapi.LoadFromFile(specPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		doc = enrich.NewDocument()
	}

	wasValid := doc.Validate() == nil

	prevIas, err := cassette.InteractionsReadFile(iaPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	tr := recorder.NewTransport(nil, prevIas)

	scaffoldNext := len(prevIas) == 0

	// Call requests that don't have a response yet
	for _, ia := range prevIas {
		if ia.Response.StatusCode > 0 {
			continue // we have a response
		}

		if ia.Request.URL == "" {
			scaffoldNext = true
			continue
		}

		req, err := ia.Request.Create(ctx)
		if err != nil {
			return err
		}

		if auth != "" {
			req.Header.Set("Authorization", auth)
		}

		if _, err := tr.RoundTrip(req); err != nil {
			return err
		}
	}

	if err := enrich.Enrich(doc, tr.Interactions); err != nil {
		return err
	}

	// Sort responses and components (but not paths to keep the order)
	for _, path := range doc.Paths {
		for _, op := range path.Operations {
			op.Responses.Sort()
		}
	}

	doc.Components.SortMaps()

	if wasValid {
		if err := doc.Validate(); err != nil {
			return fmt.Errorf("produced invalid doc: %w", err)
		}
	}

	if err := doc.WriteToFile(specPath); err != nil {
		return err
	}

	ias := tr.Interactions

	if strings.HasPrefix(doc.Info.Title, "Habitica") {
		// for them, "X-Client" is more like a user agent - they're weird that way
		m := cassette.DefaultMasker()
		if i := slices.Index(m.HeaderKeys, "X-Client"); i > -1 {
			m.HeaderKeys = slices.Delete(m.HeaderKeys, i, i+1)
		}

		ias.MaskWith(m)
	} else {
		ias.Mask()
	}

	ias.TrimResponseHeaders()

	if trimExamples > 0 {
		ias.TrimResponseBodies(trimExamples)
	}

	if scaffoldNext {
		ias = append(ias, cassette.Interaction{})
	}

	if err := ias.WriteFile(iaPath); err != nil {
		return err
	}

	return nil
}
