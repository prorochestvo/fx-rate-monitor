package sourceaudit

import (
	"testing"
	"testing/fstest"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realSeedLine is a verbatim copy of the first line of 202605.007.rate_sources.seed_initial.sql
// to confirm the tokenizer round-trips correctly with real regex patterns.
const realSeedLine = `INSERT OR IGNORE INTO rate_sources VALUES('KZ_BCC_BID_USD_KZT','Center Credit Bank','USD','KZT','https://www.bcc.kz/kz/','6h','BID',1,'{}','[{"method":"regex","pattern":"USD \\/ KZT[\\s\\S]{1,500}?<div class=\"flex-x justify-end\">(\\d+\\.\\d+)<\\/div>"}]');`

func TestParseSeedFiles(t *testing.T) {
	t.Parallel()

	t.Run("happy path with two rows", func(t *testing.T) {
		t.Parallel()

		content := `
-- comment line

INSERT OR IGNORE INTO rate_sources VALUES('SRC_REGEX','Acme Bank','USD','KZT','https://example.com/','6h','BID',1,'{}','[{"method":"regex","pattern":"rate=([\\d.]+)"}]');
INSERT OR IGNORE INTO rate_sources VALUES('SRC_JSON','Acme Bank','EUR','KZT','https://example.com/json','1h','ASK',0,'{}','[{"method":"json","pattern":"data.rate"}]');
`
		fsys := fstest.MapFS{
			"migrations/test.seed.sql": {Data: []byte(content)},
		}

		sources, err := ParseSeedFiles(fsys, "migrations/test.seed.sql")
		require.NoError(t, err)
		require.Len(t, sources, 2)

		s0 := sources[0]
		assert.Equal(t, "SRC_REGEX", s0.Name)
		assert.Equal(t, "Acme Bank", s0.Vendor)
		assert.Equal(t, "USD", s0.Base)
		assert.Equal(t, "KZT", s0.Quote)
		assert.Equal(t, "https://example.com/", s0.URL)
		assert.Equal(t, "6h", s0.Interval)
		assert.Equal(t, "BID", s0.Side)
		assert.True(t, s0.Active)
		require.Len(t, s0.Rules, 1)
		assert.Equal(t, domain.MethodRegex, s0.Rules[0].Method)
		assert.Equal(t, `rate=([\d.]+)`, s0.Rules[0].Pattern)
		assert.Equal(t, "test.seed.sql:4", s0.Origin)

		s1 := sources[1]
		assert.Equal(t, "SRC_JSON", s1.Name)
		assert.Equal(t, "EUR", s1.Base)
		assert.Equal(t, "ASK", s1.Side)
		assert.False(t, s1.Active)
		require.Len(t, s1.Rules, 1)
		assert.Equal(t, domain.MethodJSONPath, s1.Rules[0].Method)
		assert.Equal(t, "data.rate", s1.Rules[0].Pattern)
	})

	t.Run("escaped quote in vendor column", func(t *testing.T) {
		t.Parallel()

		content := `INSERT OR IGNORE INTO rate_sources VALUES('SRC1','O''Brien Bank','USD','KZT','https://x.com/','6h','BID',1,'{}','[{"method":"regex","pattern":"(\\d+)"}]');`
		fsys := fstest.MapFS{
			"a.seed.sql": {Data: []byte(content)},
		}

		sources, err := ParseSeedFiles(fsys, "a.seed.sql")
		require.NoError(t, err)
		require.Len(t, sources, 1)
		assert.Equal(t, "O'Brien Bank", sources[0].Vendor)
	})

	t.Run("malformed row - 9 columns", func(t *testing.T) {
		t.Parallel()

		content := `INSERT OR IGNORE INTO rate_sources VALUES('SRC1','Vendor','USD','KZT','https://x.com/','6h','BID',1,'{}');`
		fsys := fstest.MapFS{
			"bad.seed.sql": {Data: []byte(content)},
		}

		_, err := ParseSeedFiles(fsys, "bad.seed.sql")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bad.seed.sql")
		assert.Contains(t, err.Error(), "1")
	})

	t.Run("malformed rules JSON", func(t *testing.T) {
		t.Parallel()

		content := `INSERT OR IGNORE INTO rate_sources VALUES('SRC1','Vendor','USD','KZT','https://x.com/','6h','BID',1,'{}','not-json');`
		fsys := fstest.MapFS{
			"badjson.seed.sql": {Data: []byte(content)},
		}

		_, err := ParseSeedFiles(fsys, "badjson.seed.sql")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "badjson.seed.sql")
		assert.Contains(t, err.Error(), "1")
	})

	t.Run("blank lines comment lines non-INSERT statements ignored", func(t *testing.T) {
		t.Parallel()

		content := `
-- this is a comment
CREATE TABLE IF NOT EXISTS foo (id INTEGER);

INSERT OR IGNORE INTO other_table VALUES(1,2,3);
`
		fsys := fstest.MapFS{
			"misc.seed.sql": {Data: []byte(content)},
		}

		sources, err := ParseSeedFiles(fsys, "misc.seed.sql")
		require.NoError(t, err)
		assert.Empty(t, sources)
	})

	t.Run("glob matches multiple files in lexicographic order", func(t *testing.T) {
		t.Parallel()

		file1 := `INSERT OR IGNORE INTO rate_sources VALUES('SRC_B','Vendor','USD','KZT','https://b.com/','6h','BID',1,'{}','[{"method":"regex","pattern":"(\\d+)"}]');`
		file2 := `INSERT OR IGNORE INTO rate_sources VALUES('SRC_A','Vendor','USD','KZT','https://a.com/','6h','ASK',1,'{}','[{"method":"regex","pattern":"(\\d+)"}]');`

		fsys := fstest.MapFS{
			"migrations/202.seed.sql": {Data: []byte(file1)},
			"migrations/101.seed.sql": {Data: []byte(file2)},
		}

		sources, err := ParseSeedFiles(fsys, "migrations/*.seed.sql")
		require.NoError(t, err)
		require.Len(t, sources, 2)
		assert.Equal(t, "SRC_A", sources[0].Name, "101.seed.sql comes first lexicographically")
		assert.Equal(t, "SRC_B", sources[1].Name)
	})

	t.Run("active=1 maps to true active=0 to false", func(t *testing.T) {
		t.Parallel()

		content := `INSERT OR IGNORE INTO rate_sources VALUES('SRC_ACTIVE','V','USD','KZT','https://x.com/','6h','BID',1,'{}','[{"method":"regex","pattern":"(\\d+)"}]');
INSERT OR IGNORE INTO rate_sources VALUES('SRC_INACTIVE','V','USD','KZT','https://x.com/','6h','BID',0,'{}','[{"method":"regex","pattern":"(\\d+)"}]');`
		fsys := fstest.MapFS{
			"active.seed.sql": {Data: []byte(content)},
		}

		sources, err := ParseSeedFiles(fsys, "active.seed.sql")
		require.NoError(t, err)
		require.Len(t, sources, 2)
		assert.True(t, sources[0].Active)
		assert.False(t, sources[1].Active)
	})

	t.Run("real seed line round-trips correctly", func(t *testing.T) {
		t.Parallel()

		fsys := fstest.MapFS{
			"real.seed.sql": {Data: []byte(realSeedLine)},
		}

		sources, err := ParseSeedFiles(fsys, "real.seed.sql")
		require.NoError(t, err)
		require.Len(t, sources, 1)

		s := sources[0]
		assert.Equal(t, "KZ_BCC_BID_USD_KZT", s.Name)
		assert.Equal(t, "Center Credit Bank", s.Vendor)
		assert.Equal(t, "BID", s.Side)
		assert.True(t, s.Active)
		require.Len(t, s.Rules, 1)
		assert.Equal(t, domain.MethodRegex, s.Rules[0].Method)
		// After SQL unquoting + JSON decoding the pattern should contain the actual regex.
		assert.Contains(t, s.Rules[0].Pattern, `USD \/ KZT`)
		assert.Contains(t, s.Rules[0].Pattern, `[\s\S]{1,500}?`)
	})
}
