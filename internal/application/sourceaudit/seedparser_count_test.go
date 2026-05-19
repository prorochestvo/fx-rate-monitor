package sourceaudit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seilbekskindirov/monitor/internal/application/sourceaudit"
	"github.com/seilbekskindirov/monitor/migrations"
)

func TestParseSeedFiles_EmbeddedMigrations(t *testing.T) {
	t.Parallel()

	t.Run("embedded migrations enumerate 36 sources", func(t *testing.T) {
		t.Parallel()
		sources, err := sourceaudit.ParseSeedFiles(migrations.MigrationsFS, "*.seed*.sql")
		require.NoError(t, err)
		assert.Len(t, sources, 36)
	})
}
