package sourceaudit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/seilbekskindirov/monitor/internal/domain"
)

// SeededSource represents a rate source record parsed from a seed SQL file.
type SeededSource struct {
	Name     string
	Vendor   string
	Base     string
	Quote    string
	URL      string
	Interval string
	Side     string
	Active   bool
	Rules    []domain.RateSourceRule
	Origin   string
}

const insertPrefix = "INSERT OR IGNORE INTO rate_sources VALUES("

// ParseSeedFiles reads all files in fsys matching glob (lexicographic order) and
// returns the parsed SeededSource records in the order they appear.
func ParseSeedFiles(fsys fs.FS, glob string) ([]SeededSource, error) {
	matches, err := fs.Glob(fsys, glob)
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", glob, err)
	}

	sort.Strings(matches)

	var out []SeededSource
	for _, path := range matches {
		records, err := parseSeedFile(fsys, path)
		if err != nil {
			return nil, err
		}
		out = append(out, records...)
	}
	return out, nil
}

func parseSeedFile(fsys fs.FS, path string) ([]SeededSource, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func(c fs.File) { _ = c.Close() }(f)

	base := filepath.Base(path)
	var out []SeededSource
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		if !strings.HasPrefix(line, insertPrefix) {
			continue
		}

		inner := line[len(insertPrefix):]
		inner = strings.TrimSuffix(inner, ");")

		tokens, err := tokenizeSQLValues(inner)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: tokenize: %w", base, lineNum, err)
		}
		if len(tokens) != 10 {
			return nil, fmt.Errorf("%s:%d: expected 10 columns, got %d", base, lineNum, len(tokens))
		}

		src, err := tokensToSeededSource(tokens)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", base, lineNum, err)
		}
		src.Origin = fmt.Sprintf("%s:%d", base, lineNum)
		out = append(out, src)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}

// tokenizeSQLValues splits a comma-separated SQL value list respecting
// single-quoted strings (with ” escapes). Returns raw tokens including quotes.
func tokenizeSQLValues(s string) ([]string, error) {
	var out []string
	var buf strings.Builder
	inString := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inString && c == '\'' && i+1 < len(s) && s[i+1] == '\'':
			buf.WriteByte('\'')
			buf.WriteByte('\'')
			i++
		case c == '\'':
			inString = !inString
			buf.WriteByte(c)
		case c == ',' && !inString:
			out = append(out, buf.String())
			buf.Reset()
		default:
			buf.WriteByte(c)
		}
	}
	if inString {
		return nil, fmt.Errorf("unterminated string literal")
	}
	if buf.Len() > 0 {
		out = append(out, buf.String())
	}
	return out, nil
}

// sqlUnquote strips the outer single quotes from a SQL string literal and
// replaces ” escape sequences with a single '.
func sqlUnquote(tok string) (string, error) {
	if !strings.HasPrefix(tok, "'") || !strings.HasSuffix(tok, "'") {
		return "", fmt.Errorf("expected quoted string, got %q", tok)
	}
	inner := tok[1 : len(tok)-1]
	return strings.ReplaceAll(inner, "''", "'"), nil
}

func tokensToSeededSource(tokens []string) (SeededSource, error) {
	name, err := sqlUnquote(tokens[0])
	if err != nil {
		return SeededSource{}, fmt.Errorf("column name: %w", err)
	}
	vendor, err := sqlUnquote(tokens[1])
	if err != nil {
		return SeededSource{}, fmt.Errorf("column vendor: %w", err)
	}
	base, err := sqlUnquote(tokens[2])
	if err != nil {
		return SeededSource{}, fmt.Errorf("column base: %w", err)
	}
	quote, err := sqlUnquote(tokens[3])
	if err != nil {
		return SeededSource{}, fmt.Errorf("column quote: %w", err)
	}
	rawURL, err := sqlUnquote(tokens[4])
	if err != nil {
		return SeededSource{}, fmt.Errorf("column url: %w", err)
	}
	interval, err := sqlUnquote(tokens[5])
	if err != nil {
		return SeededSource{}, fmt.Errorf("column interval: %w", err)
	}
	side, err := sqlUnquote(tokens[6])
	if err != nil {
		return SeededSource{}, fmt.Errorf("column side: %w", err)
	}

	activeInt, err := strconv.Atoi(strings.TrimSpace(tokens[7]))
	if err != nil {
		return SeededSource{}, fmt.Errorf("column active: %w", err)
	}
	var active bool
	switch activeInt {
	case 1:
		active = true
	case 0:
		active = false
	default:
		return SeededSource{}, fmt.Errorf("column active: unexpected value %d (must be 0 or 1)", activeInt)
	}

	_, err = sqlUnquote(tokens[8])
	if err != nil {
		return SeededSource{}, fmt.Errorf("column headers_json: %w", err)
	}

	rulesRaw, err := sqlUnquote(tokens[9])
	if err != nil {
		return SeededSource{}, fmt.Errorf("column rules_json: %w", err)
	}
	var rules []domain.RateSourceRule
	if err = json.Unmarshal([]byte(rulesRaw), &rules); err != nil {
		return SeededSource{}, fmt.Errorf("column rules_json: decode JSON: %w", err)
	}

	return SeededSource{
		Name:     name,
		Vendor:   vendor,
		Base:     base,
		Quote:    quote,
		URL:      rawURL,
		Interval: interval,
		Side:     side,
		Active:   active,
		Rules:    rules,
	}, nil
}
