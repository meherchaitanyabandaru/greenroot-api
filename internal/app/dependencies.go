package app

import (
	"database/sql"
	"log/slog"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/logger"
	platformemail "github.com/meherchaitanyabandaru/greenroot-api/platform/email"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	platformsms "github.com/meherchaitanyabandaru/greenroot-api/platform/sms"
	"github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
	"github.com/redis/go-redis/v9"
)

type Dependencies struct {
	Config     config.Config
	Logger     *slog.Logger
	LogManager *logger.Manager
	DB         *sql.DB
	JWT        *jwtplatform.Service
	Storage    *storage.Client
	Audit      *auditlog.Service
	// Redis is nil when REDIS_ADDR is not configured; all callers must nil-check.
	Redis *redis.Client
	SMS   platformsms.Provider
	Email platformemail.Provider
}
