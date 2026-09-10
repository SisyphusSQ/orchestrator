package metadata

import (
	"github.com/openark/orchestrator/internal/repository/database"
)

func ReadTimeNow() (string, error) {
	return database.ReadTimeNow()
}
