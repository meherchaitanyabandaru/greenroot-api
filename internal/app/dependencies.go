package app

import (
	"database/sql"
	"log/slog"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/logger"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
)

type Dependencies struct {
	Config     config.Config
	Logger     *slog.Logger
	LogManager *logger.Manager
	DB         *sql.DB
	JWT        *jwtplatform.Service
	Storage    *storage.Client
	Audit      *auditlog.Service
}
