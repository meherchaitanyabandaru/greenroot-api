package invites

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	Create(ctx context.Context, actorID int64, req CreateInviteRequest) (*Invite, error)
	FindByUUID(ctx context.Context, uuid string) (*Invite, error)
	Accept(ctx context.Context, uuid string, acceptedByUserID int64) (*Invite, error)
	Cancel(ctx context.Context, uuid string, actorID int64) (*Invite, error)
	ListByNursery(ctx context.Context, nurseryID int64) ([]Invite, error)
	CreateAuditLog(ctx context.Context, input CreateAuditInput) error
	UserOwnsNursery(ctx context.Context, userID int64) (bool, error)
	IsNurseryOwner(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	UserIsManager(ctx context.Context, userID int64) (bool, error)
	AddNurseryMember(ctx context.Context, nurseryID int64, userID int64, role string, invitedByUserID int64) error
	GrantNurseryOwnerRole(ctx context.Context, userID int64) error
}

type CreateAuditInput struct {
	TableName string
	RecordID  int64
	Action    string
	ChangedBy int64
	SourceIP  string
	UserAgent string
	NewJSON   string
	At        time.Time
}

type PostgresRepository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Create(ctx context.Context, actorID int64, req CreateInviteRequest) (*Invite, error) {
	const query = `
		INSERT INTO public.invites (
			invite_type, invited_by_user_id, nursery_id, role,
			target_mobile, target_email, target_name,
			status, expires_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'PENDING', CURRENT_TIMESTAMP + INTERVAL '7 days', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING id
	`
	var id int64
	if err := r.db.QueryRowContext(ctx, query,
		req.InviteType, actorID,
		int64OrNil(req.NurseryID),
		req.Role, req.TargetMobile, req.TargetEmail, req.TargetName,
	).Scan(&id); err != nil {
		return nil, err
	}
	return r.findByID(ctx, id)
}

func (r *PostgresRepository) FindByUUID(ctx context.Context, uuid string) (*Invite, error) {
	invite, err := scanInvite(r.db.QueryRowContext(ctx, baseSelect()+" WHERE i.invite_uuid = $1::uuid", uuid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &invite, err
}

func (r *PostgresRepository) Accept(ctx context.Context, uuid string, acceptedByUserID int64) (*Invite, error) {
	const query = `
		UPDATE public.invites
		SET status = 'ACCEPTED', accepted_by_user_id = $2, accepted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE invite_uuid = $1::uuid AND status = 'PENDING' AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		RETURNING id
	`
	var id int64
	if err := r.db.QueryRowContext(ctx, query, uuid, acceptedByUserID).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return r.findByID(ctx, id)
}

func (r *PostgresRepository) Cancel(ctx context.Context, uuid string, actorID int64) (*Invite, error) {
	const query = `
		UPDATE public.invites
		SET status = 'CANCELLED', updated_at = CURRENT_TIMESTAMP
		WHERE invite_uuid = $1::uuid AND status = 'PENDING'
		RETURNING id
	`
	var id int64
	if err := r.db.QueryRowContext(ctx, query, uuid).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return r.findByID(ctx, id)
}

func (r *PostgresRepository) ListByNursery(ctx context.Context, nurseryID int64) ([]Invite, error) {
	rows, err := r.db.QueryContext(ctx, baseSelect()+" WHERE i.nursery_id = $1 ORDER BY i.id DESC", nurseryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	invites := make([]Invite, 0)
	for rows.Next() {
		invite, err := scanInviteRows(rows)
		if err != nil {
			return nil, err
		}
		invites = append(invites, invite)
	}
	return invites, rows.Err()
}

func (r *PostgresRepository) findByID(ctx context.Context, id int64) (*Invite, error) {
	invite, err := scanInvite(r.db.QueryRowContext(ctx, baseSelect()+" WHERE i.id = $1", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &invite, err
}

func (r *PostgresRepository) CreateAuditLog(ctx context.Context, input CreateAuditInput) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO public.audit_logs (table_name, record_id, action_type, old_data, new_data, changed_by, source_ip, user_agent, changed_at)
		VALUES ($1, $2, $3, NULL, NULLIF($4, '')::jsonb, $5, NULLIF($6, ''), NULLIF($7, ''), $8)
	`, input.TableName, input.RecordID, input.Action, input.NewJSON, input.ChangedBy, input.SourceIP, input.UserAgent, input.At)
	return err
}

func (r *PostgresRepository) UserOwnsNursery(ctx context.Context, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM public.nurseries WHERE owner_user_id = $1 AND COALESCE(status::text,'') <> 'DELETED')`,
		userID,
	).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) IsNurseryOwner(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM public.nurseries WHERE nursery_id = $1 AND owner_user_id = $2 AND COALESCE(status::text,'') <> 'DELETED')`,
		nurseryID, userID,
	).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) UserIsManager(ctx context.Context, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM public.nursery_users WHERE user_id = $1 AND COALESCE(status, 'ACTIVE') = 'ACTIVE' AND COALESCE(is_active, true) = true)`,
		userID,
	).Scan(&exists)
	return exists, err
}

func baseSelect() string {
	return `
		SELECT i.id, i.invite_uuid::text, i.invite_type, i.invited_by_user_id,
			i.nursery_id, n.nursery_name, i.role,
			i.target_mobile, i.target_email, i.target_name,
			i.status, i.expires_at, i.accepted_by_user_id, i.accepted_at,
			i.created_at, i.updated_at
		FROM public.invites i
		LEFT JOIN public.nurseries n ON n.nursery_id = i.nursery_id
	`
}

func scanInviteRows(rows *sql.Rows) (Invite, error) {
	return scanInvite(rows)
}

func scanInvite(row interface{ Scan(dest ...any) error }) (Invite, error) {
	var invite Invite
	var nurseryID, acceptedByUserID sql.NullInt64
	var nurseryName, role, targetMobile, targetEmail, targetName sql.NullString
	var expiresAt, acceptedAt sql.NullTime
	if err := row.Scan(
		&invite.ID, &invite.InviteUUID, &invite.InviteType, &invite.InvitedByUserID,
		&nurseryID, &nurseryName, &role,
		&targetMobile, &targetEmail, &targetName,
		&invite.Status, &expiresAt, &acceptedByUserID, &acceptedAt,
		&invite.CreatedAt, &invite.UpdatedAt,
	); err != nil {
		return Invite{}, err
	}
	invite.NurseryID = nullableInt64(nurseryID)
	invite.NurseryName = nullableString(nurseryName)
	invite.Role = nullableString(role)
	invite.TargetMobile = nullableString(targetMobile)
	invite.TargetEmail = nullableString(targetEmail)
	invite.TargetName = nullableString(targetName)
	if expiresAt.Valid {
		invite.ExpiresAt = &expiresAt.Time
	}
	invite.AcceptedByUserID = nullableInt64(acceptedByUserID)
	if acceptedAt.Valid {
		invite.AcceptedAt = &acceptedAt.Time
	}
	return invite, nil
}

func nullableString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func nullableInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func int64OrNil(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func (r *PostgresRepository) AddNurseryMember(ctx context.Context, nurseryID int64, userID int64, role string, invitedByUserID int64) error {
	if role == "" {
		role = "MANAGER"
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO public.nursery_users (nursery_id, user_id, role, invited_by_user_id, status, joined_at, updated_at)
		SELECT $1, $2, $3, $4, 'ACTIVE', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		WHERE NOT EXISTS (
			SELECT 1 FROM public.nursery_users WHERE nursery_id = $1 AND user_id = $2 AND nursery_role_id IS NULL
		)
	`, nurseryID, userID, role, invitedByUserID); err != nil {
		return err
	}
	// Grant the MANAGER role in user_roles so the JWT includes it and API checks pass.
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO public.user_roles (user_id, role_id)
		SELECT $1, role_id FROM public.roles WHERE role_code = 'MANAGER'
		ON CONFLICT DO NOTHING
	`, userID)
	return err
}

func (r *PostgresRepository) GrantNurseryOwnerRole(ctx context.Context, userID int64) error {
	const nurseryOwnerRoleID = 3
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO public.user_roles (user_id, role_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, userID, nurseryOwnerRoleID)
	return err
}
