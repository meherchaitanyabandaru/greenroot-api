package requests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	List(ctx context.Context, input ListRequestsRequest) ([]PlantRequest, int64, error)
	FindByID(ctx context.Context, requestID int64) (*PlantRequest, error)
	Create(ctx context.Context, actorID int64, input CreateRequest) (*PlantRequest, error)
	Update(ctx context.Context, actorID int64, requestID int64, input UpdateRequest) (*PlantRequest, error)
	UpdateStatus(ctx context.Context, requestID int64, status string) (*PlantRequest, error)
	Delete(ctx context.Context, requestID int64) error
	ListResponses(ctx context.Context, requestID int64) ([]Response, error)
	CreateResponse(ctx context.Context, requestID int64, actorID int64, input CreateResponseRequest) (*Response, error)
	UpdateResponse(ctx context.Context, responseID int64, input UpdateResponseRequest) (*Response, error)
	RecomputeRequestStatus(ctx context.Context, requestID int64) error
	InventoryAvailable(ctx context.Context, supplierNurseryID int64, plantID int64, sizeID *int16) (int, error)
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
}

type PostgresRepository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) List(ctx context.Context, input ListRequestsRequest) ([]PlantRequest, int64, error) {
	where, args := buildWhere(input)
	var total int64
	if err := r.db.QueryRowContext(ctx, baseCount()+` `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (input.Page - 1) * input.PerPage
	args = append(args, input.PerPage, offset)
	query := fmt.Sprintf(baseSelect()+`
		%s
		ORDER BY pr.request_id DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	requests := make([]PlantRequest, 0)
	for rows.Next() {
		request, err := scanRequestRows(rows)
		if err != nil {
			return nil, 0, err
		}
		requests = append(requests, request)
	}
	return requests, total, rows.Err()
}

func (r *PostgresRepository) FindByID(ctx context.Context, requestID int64) (*PlantRequest, error) {
	request, err := scanRequestRow(r.db.QueryRowContext(ctx, baseSelect()+` WHERE pr.request_id = $1`, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	request.Responses, _ = r.ListResponses(ctx, request.ID)
	return request, nil
}

func (r *PostgresRepository) Create(ctx context.Context, actorID int64, input CreateRequest) (*PlantRequest, error) {
	now := time.Now()
	requestCode, err := publiccode.Next(ctx, r.db, publiccode.Requests, now)
	if err != nil {
		return nil, err
	}

	const query = `
		INSERT INTO public.plant_requests (
			request_code, requesting_nursery_id, requested_by_user_id, plant_id, size_id, quantity_required,
			radius_km, required_by_date, notes, status, expires_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), $10, $11, $12, $12)
		RETURNING request_id
	`
	var requestID int64
	if err := r.db.QueryRowContext(
		ctx,
		query,
		requestCode,
		input.RequestingNurseryID,
		actorID,
		input.PlantID,
		int16OrNil(input.SizeID),
		input.QuantityRequired,
		input.RadiusKM,
		input.RequiredByDate,
		stringOrEmpty(input.Notes),
		input.Status,
		input.ExpiresAt,
		now,
	).Scan(&requestID); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, requestID)
}

func (r *PostgresRepository) Update(ctx context.Context, actorID int64, requestID int64, input UpdateRequest) (*PlantRequest, error) {
	const query = `
		UPDATE public.plant_requests
		SET requesting_nursery_id = $2,
			requested_by_user_id = $3,
			plant_id = $4,
			size_id = $5,
			quantity_required = $6,
			radius_km = $7,
			required_by_date = $8,
			notes = NULLIF($9, ''),
			status = $10,
			expires_at = $11,
			fulfilled_at = CASE WHEN $12 = 'CLOSED' THEN COALESCE(fulfilled_at, CURRENT_TIMESTAMP) ELSE fulfilled_at END,
			updated_at = CURRENT_TIMESTAMP
		WHERE request_id = $1
	`
	result, err := r.db.ExecContext(
		ctx,
		query,
		requestID,
		input.RequestingNurseryID,
		actorID,
		input.PlantID,
		int16OrNil(input.SizeID),
		input.QuantityRequired,
		input.RadiusKM,
		input.RequiredByDate,
		stringOrEmpty(input.Notes),
		input.Status,
		input.ExpiresAt,
		input.Status,
	)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, requestID)
}

func (r *PostgresRepository) UpdateStatus(ctx context.Context, requestID int64, status string) (*PlantRequest, error) {
	query := `
		UPDATE public.plant_requests
		SET status = $2,
			fulfilled_at = CASE WHEN $3 = 'CLOSED' THEN COALESCE(fulfilled_at, CURRENT_TIMESTAMP) ELSE fulfilled_at END,
			updated_at = CURRENT_TIMESTAMP
		WHERE request_id = $1
	`
	result, err := r.db.ExecContext(ctx, query, requestID, status, status)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, requestID)
}

func (r *PostgresRepository) Delete(ctx context.Context, requestID int64) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE public.plant_requests SET status = 'REJECTED', updated_at = CURRENT_TIMESTAMP WHERE request_id = $1 AND status <> 'REJECTED'`,
		requestID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ListResponses(ctx context.Context, requestID int64) ([]Response, error) {
	rows, err := r.db.QueryContext(ctx, responseSelect()+` WHERE prr.request_id = $1 ORDER BY prr.response_id DESC`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	responses := make([]Response, 0)
	for rows.Next() {
		response, err := scanResponseRows(rows)
		if err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}
	return responses, rows.Err()
}

func (r *PostgresRepository) CreateResponse(ctx context.Context, requestID int64, actorID int64, input CreateResponseRequest) (*Response, error) {
	const query = `
		INSERT INTO public.plant_request_responses (
			request_id, supplier_nursery_id, responded_by_user_id, available_quantity, remarks, status, created_at
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, CURRENT_TIMESTAMP)
		ON CONFLICT (request_id, supplier_nursery_id)
		DO UPDATE SET responded_by_user_id = EXCLUDED.responded_by_user_id,
			available_quantity = EXCLUDED.available_quantity,
			remarks = EXCLUDED.remarks,
			status = EXCLUDED.status
		RETURNING response_id
	`
	var responseID int64
	if err := r.db.QueryRowContext(ctx, query, requestID, input.SupplierNurseryID, actorID, input.AvailableQuantity, stringOrEmpty(input.Remarks), input.Status).Scan(&responseID); err != nil {
		return nil, err
	}
	return r.findResponse(ctx, responseID)
}

func (r *PostgresRepository) UpdateResponse(ctx context.Context, responseID int64, input UpdateResponseRequest) (*Response, error) {
	const query = `
		UPDATE public.plant_request_responses
		SET status = $2
		WHERE response_id = $1
	`
	result, err := r.db.ExecContext(ctx, query, responseID, input.Status)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrNotFound
	}
	return r.findResponse(ctx, responseID)
}

// RecomputeRequestStatus recalculates and persists the request status based on accepted response quantities.
// DRAFT and CLOSED requests are not touched.
func (r *PostgresRepository) RecomputeRequestStatus(ctx context.Context, requestID int64) error {
	var requiredQty int
	var currentStatus string
	if err := r.db.QueryRowContext(ctx,
		`SELECT quantity_required, status FROM public.plant_requests WHERE request_id = $1`,
		requestID).Scan(&requiredQty, &currentStatus); err != nil {
		return err
	}
	if currentStatus == "DRAFT" || currentStatus == "CLOSED" {
		return nil
	}

	var acceptedQty int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(available_quantity), 0) FROM public.plant_request_responses WHERE request_id = $1 AND status = 'ACCEPTED'`,
		requestID).Scan(&acceptedQty); err != nil {
		return err
	}

	var totalResponses, terminalResponses int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE status IN ('REJECTED', 'NOT_AVAILABLE')) FROM public.plant_request_responses WHERE request_id = $1`,
		requestID).Scan(&totalResponses, &terminalResponses); err != nil {
		return err
	}

	var newStatus string
	switch {
	case acceptedQty >= requiredQty:
		newStatus = "ACCEPTED"
	case acceptedQty > 0:
		newStatus = "PARTIALLY_ACCEPTED"
	case totalResponses > 0 && totalResponses == terminalResponses:
		newStatus = "REJECTED"
	default:
		newStatus = "OPEN"
	}

	_, err := r.db.ExecContext(ctx,
		`UPDATE public.plant_requests SET status = $2, updated_at = CURRENT_TIMESTAMP WHERE request_id = $1`,
		requestID, newStatus)
	return err
}

func (r *PostgresRepository) InventoryAvailable(ctx context.Context, supplierNurseryID int64, plantID int64, sizeID *int16) (int, error) {
	query := `
		SELECT COALESCE(SUM(available_quantity), 0)
		FROM public.nursery_inventory
		WHERE nursery_id = $1 AND plant_id = $2 AND inventory_status IN ('AVAILABLE', 'LOW_STOCK')
	`
	args := []any{supplierNurseryID, plantID}
	if sizeID != nil {
		args = append(args, *sizeID)
		query += fmt.Sprintf(" AND size_id = $%d", len(args))
	}
	var available int
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&available)
	return available, err
}

func (r *PostgresRepository) IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM public.nursery_users
		WHERE nursery_id = $1 AND user_id = $2 AND COALESCE(is_active, true) = true
		UNION ALL
		SELECT 1 FROM public.nurseries
		WHERE nursery_id = $1 AND owner_user_id = $2 AND COALESCE(status::text, '') <> 'DELETED'
	)`, nurseryID, userID).Scan(&exists)
	return exists, err
}


func baseSelect() string {
	return `
		SELECT pr.request_id, pr.request_code, pr.requesting_nursery_id, n.nursery_name, pr.requested_by_user_id,
			u.first_name, pr.plant_id, p.scientific_name, p.common_name, pr.size_id, ps.size_code,
			ps.display_name, pr.quantity_required, pr.radius_km, pr.required_by_date, pr.notes, pr.status::text,
			pr.expires_at, pr.fulfilled_at, pr.created_at, pr.updated_at
		FROM public.plant_requests pr
		JOIN public.nurseries n ON n.nursery_id = pr.requesting_nursery_id
		JOIN public.users u ON u.user_id = pr.requested_by_user_id
		JOIN public.plants p ON p.plant_id = pr.plant_id
		LEFT JOIN public.plant_sizes ps ON ps.size_id = pr.size_id
	`
}

func baseCount() string {
	return `
		SELECT COUNT(*)
		FROM public.plant_requests pr
		JOIN public.nurseries n ON n.nursery_id = pr.requesting_nursery_id
		JOIN public.plants p ON p.plant_id = pr.plant_id
	`
}

func responseSelect() string {
	return `
		SELECT prr.response_id, prr.request_id, prr.supplier_nursery_id, n.nursery_name,
			prr.responded_by_user_id, u.first_name, prr.available_quantity, prr.remarks,
			prr.status::text, prr.created_at
		FROM public.plant_request_responses prr
		JOIN public.nurseries n ON n.nursery_id = prr.supplier_nursery_id
		JOIN public.users u ON u.user_id = prr.responded_by_user_id
	`
}

func buildWhere(input ListRequestsRequest) (string, []any) {
	clauses := []string{"p.is_active = true"}
	args := make([]any, 0)
	if input.NurseryID > 0 {
		args = append(args, input.NurseryID)
		clauses = append(clauses, fmt.Sprintf("pr.requesting_nursery_id = $%d", len(args)))
	}
	if input.PlantID > 0 {
		args = append(args, input.PlantID)
		clauses = append(clauses, fmt.Sprintf("pr.plant_id = $%d", len(args)))
	}
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("pr.status::text = $%d", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf("(pr.request_code ILIKE $%d OR n.nursery_name ILIKE $%d OR p.scientific_name ILIKE $%d OR p.common_name ILIKE $%d OR pr.notes ILIKE $%d)", len(args), len(args), len(args), len(args), len(args)))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func (r *PostgresRepository) findResponse(ctx context.Context, responseID int64) (*Response, error) {
	response, err := scanResponseRow(r.db.QueryRowContext(ctx, responseSelect()+` WHERE prr.response_id = $1`, responseID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return response, err
}

func scanRequestRow(row interface{ Scan(dest ...any) error }) (*PlantRequest, error) {
	request, err := scanRequest(row)
	if err != nil {
		return nil, err
	}
	return &request, nil
}

func scanRequestRows(rows *sql.Rows) (PlantRequest, error) {
	return scanRequest(rows)
}

func scanRequest(row interface{ Scan(dest ...any) error }) (PlantRequest, error) {
	var request PlantRequest
	var commonName, sizeCode, sizeName, notes sql.NullString
	var sizeID sql.NullInt16
	var requiredByDate, expiresAt, fulfilledAt sql.NullTime
	if err := row.Scan(
		&request.ID,
		&request.RequestCode,
		&request.RequestingNurseryID,
		&request.RequestingNursery,
		&request.RequestedByUserID,
		&request.RequestedByName,
		&request.PlantID,
		&request.ScientificName,
		&commonName,
		&sizeID,
		&sizeCode,
		&sizeName,
		&request.QuantityRequired,
		&request.RadiusKM,
		&requiredByDate,
		&notes,
		&request.Status,
		&expiresAt,
		&fulfilledAt,
		&request.CreatedAt,
		&request.UpdatedAt,
	); err != nil {
		return PlantRequest{}, err
	}
	request.CommonName = nullableString(commonName)
	if sizeID.Valid {
		request.SizeID = &sizeID.Int16
	}
	request.SizeCode = nullableString(sizeCode)
	request.SizeName = nullableString(sizeName)
	request.Notes = nullableString(notes)
	if requiredByDate.Valid {
		request.RequiredByDate = &requiredByDate.Time
	}
	if expiresAt.Valid {
		request.ExpiresAt = &expiresAt.Time
	}
	if fulfilledAt.Valid {
		request.FulfilledAt = &fulfilledAt.Time
	}
	return request, nil
}

func scanResponseRow(row interface{ Scan(dest ...any) error }) (*Response, error) {
	response, err := scanResponse(row)
	if err != nil {
		return nil, err
	}
	return &response, nil
}

func scanResponseRows(rows *sql.Rows) (Response, error) {
	return scanResponse(rows)
}

func scanResponse(row interface{ Scan(dest ...any) error }) (Response, error) {
	var response Response
	var remarks sql.NullString
	if err := row.Scan(&response.ID, &response.RequestID, &response.SupplierNurseryID, &response.SupplierNursery, &response.RespondedByUserID, &response.RespondedByName, &response.AvailableQuantity, &remarks, &response.Status, &response.CreatedAt); err != nil {
		return Response{}, err
	}
	response.Remarks = nullableString(remarks)
	return response, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int16OrNil(value *int16) any {
	if value == nil {
		return nil
	}
	return *value
}
