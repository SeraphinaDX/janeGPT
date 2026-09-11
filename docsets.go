package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func listDocsets(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read docsets_dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".docset") {
			names = append(names, strings.TrimSuffix(e.Name(), ".docset"))
		}
	}
	return names, nil
}

func docsetCommand(args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("janeGPT docset", flag.ContinueOnError)
	root := fs.String("root", "", "Zeal docset storage directory")
	list := fs.Bool("list", false, "list installed docsets as JSON")
	pandoc := fs.String("pandoc", "pandoc", "Pandoc executable")
	maxBytes := fs.Int64("max-bytes", 48<<20, "maximum source HTML bytes and each complete export")
	contextBytes := fs.Int64("context-bytes", 128<<10, "maximum selected documentation context")
	timeout := fs.Duration("timeout", 9*time.Minute, "total export timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *root == "" || *maxBytes <= 0 || *contextBytes <= 0 || *timeout <= 0 || fs.NArg() != 0 {
		return errors.New("docset requires -root and positive limits")
	}
	expanded, err := expandHome(*root)
	if err != nil {
		return err
	}
	if *list {
		names, err := listDocsets(expanded)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(names)
	}
	var request struct {
		Docset string `json:"docset"`
		Query  string `json:"query"`
	}
	data, err := readLimited(in, 32<<10)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &request); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := exportDocset(ctx, expanded, request.Docset, request.Query, *pandoc, *maxBytes, *contextBytes)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}

type docPage struct {
	name, source string
	score        int
}

func exportDocset(ctx context.Context, root, name, query, pandoc string, maxBytes, contextBytes int64) (toolResult, error) {
	names, err := listDocsets(root)
	if err != nil {
		return toolResult{}, err
	}
	found := false
	for _, n := range names {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		return toolResult{}, fmt.Errorf("docset %q is not installed; available: %s", name, strings.Join(names, ", "))
	}
	// Never allow a model-selected name or a docset symlink to escape storage.
	docs := filepath.Join(root, name+".docset")
	for _, segment := range []string{"Contents", "Resources", "Documents"} {
		docs = filepath.Join(docs, segment)
		info, err := os.Lstat(docs)
		if err != nil {
			return toolResult{}, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return toolResult{}, errors.New("docset contains a symlink or invalid Documents directory")
		}
	}
	terms := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var pages []docPage
	var all strings.Builder
	err = filepath.WalkDir(docs, func(p string, e os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cannot export complete docset with symlink %s", p)
		}
		if e.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".html" && ext != ".htm" && ext != ".xhtml" {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("nonregular documentation page %s", p)
		}
		remaining := maxBytes - int64(all.Len())
		if info.Size() > remaining {
			return errors.New("complete docset exceeds max-bytes; no partial export produced")
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		data, err := readLimited(f, remaining)
		f.Close()
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			return fmt.Errorf("non-UTF-8 page %s; convert docset encoding before export", p)
		}
		relative, _ := filepath.Rel(docs, p)
		relative = filepath.ToSlash(relative)
		source := "<h1>Source page: " + html.EscapeString(relative) + "</h1>\n" + string(data) + "\n"
		if int64(len(source))+int64(all.Len()) > maxBytes {
			return errors.New("complete docset exceeds max-bytes; no partial export produced")
		}
		all.WriteString(source)
		lower := strings.ToLower(htmlToText(string(data)))
		score := 0
		for _, term := range terms {
			if len(term) < 2 {
				continue
			}
			if strings.Contains(strings.ToLower(relative), term) {
				score += 20
			}
			score += min(strings.Count(lower, term), 20)
		}
		pages = append(pages, docPage{relative, source, score})
		return nil
	})
	if err != nil {
		return toolResult{}, err
	}
	if len(pages) == 0 {
		return toolResult{}, errors.New("docset contains no HTML documentation pages")
	}
	convert := func(source, format string) (string, error) {
		data, err := captureCommand(ctx, []string{pandoc, "--sandbox", "--from=html", "--to=" + format, "--wrap=none"}, []byte(source), maxBytes)
		if err != nil {
			return "", fmt.Errorf("Pandoc %s: %w", format, err)
		}
		return string(data), nil
	}
	md, err := convert(all.String(), "gfm")
	if err != nil {
		return toolResult{}, err
	}
	org, err := convert(all.String(), "org")
	if err != nil {
		return toolResult{}, err
	}
	if strings.TrimSpace(md) == "" || strings.TrimSpace(org) == "" {
		return toolResult{}, errors.New("empty docset conversion")
	}
	// Rank only the model context. Both attachment exports above contain every page.
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].score > pages[j].score })
	var selected strings.Builder
	budget := contextBytes * 2
	for _, p := range pages {
		if int64(selected.Len()) >= budget {
			break
		}
		selected.WriteString(p.source)
	}
	excerpt, err := convert(selected.String(), "gfm")
	if err != nil {
		return toolResult{}, err
	}
	if int64(len(excerpt)) > contextBytes {
		excerpt = excerpt[:contextBytes]
		for !utf8.ValidString(excerpt) {
			excerpt = excerpt[:len(excerpt)-1]
		}
	}
	context := fmt.Sprintf("Docset: %s. Exported all %d HTML pages to %s.md and %s.org. Selected reference excerpts follow (may be incomplete). Explain the requested topic with practical examples.\n\n%s", name, len(pages), name, name, excerpt)
	return toolResult{Context: context, Attachments: []toolFile{
		{Name: name + ".md", ContentType: "text/markdown", Text: md},
		{Name: name + ".org", ContentType: "text/org", Text: org},
	}}, nil
}
