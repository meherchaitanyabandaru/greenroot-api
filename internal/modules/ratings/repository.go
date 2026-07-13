package ratings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("rating not found")

type Repository interface {
	UpsertApp(ctx context.Context, userID int64, req SubmitAppRatingRequest) (*Rating, error)
	UpsertTrip(ctx context.Context, userID int64, dispatchID int64, req SubmitTripRatingRequest) (*Rating, error)
	UpsertOrder(ctx context.Context, userID int64, orderID int64, req SubmitOrderRatingRequest) (*Rating, error)
	List(ctx context.Context, input ListRatingsRequest) ([]Rating, error)
	GetApp(ctx context.Context, userID int64) (*Rating, error)
	GetTrip(ctx context.Context, userID int64, dispatchID int64) (*Rating, error)
	GetOrder(ctx context.Context, userID int64, orderID int64) (*Rating, error)
}

type PostgresRepository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) UpsertApp(ctx context.Context, userID int64, req SubmitAppRatingRequest) (*Rating, error) {
	// Partial unique index: (rated_by_user_id) WHERE rating_type = 'APP'
	const q = `
		INSERT INTO public.ratings (rating_type, rated_by_user_id, overall_rating, would_recommend, comment, updated_at)
		VALUES ('APP', $1, $2, $3, NULLIF($4, ''), NOW())
		ON CONFLICT (rated_by_user_id) WHERE rating_type = 'APP'
		DO UPDATE SET
			overall_rating  = EXCLUDED.overall_rating,
			would_recommend = EXCLUDED.would_recommend,
			comment         = EXCLUDED.comment,
			updated_at      = NOW()
		RETURNING rating_id
	`
	var id int64
	if err := r.db.QueryRowContext(ctx, q, userID, req.OverallRating, req.WouldRecommend, req.Comment).Scan(&id); err != nil {
		return nil, err
	}
	return r.findByID(ctx, id)
}

func (r *PostgresRepository) UpsertTrip(ctx context.Context, userID int64, dispatchID int64, req SubmitTripRatingRequest) (*Rating, error) {
	// Partial unique index: (rated_by_user_id, dispatch_id) WHERE rating_type = 'TRIP'
	const q = `
		INSERT INTO public.ratings
			(rating_type, rated_by_user_id, dispatch_id, driver_behaviour_rating, on_time_delivery_rating, plant_condition_rating, comment, updated_at)
		VALUES ('TRIP', $1, $2, $3, $4, $5, NULLIF($6, ''), NOW())
		ON CONFLICT (rated_by_user_id, dispatch_id) WHERE rating_type = 'TRIP' AND dispatch_id IS NOT NULL
		DO UPDATE SET
			driver_behaviour_rating = EXCLUDED.driver_behaviour_rating,
			on_time_delivery_rating = EXCLUDED.on_time_delivery_rating,
			plant_condition_rating  = EXCLUDED.plant_condition_rating,
			comment                 = EXCLUDED.comment,
			updated_at              = NOW()
		RETURNING rating_id
	`
	var id int64
	if err := r.db.QueryRowContext(ctx, q, userID, dispatchID, req.DriverBehaviour, req.OnTimeDelivery, req.PlantCondition, req.Comment).Scan(&id); err != nil {
		return nil, err
	}
	return r.findByID(ctx, id)
}

func (r *PostgresRepository) UpsertOrder(ctx context.Context, userID int64, orderID int64, req SubmitOrderRatingRequest) (*Rating, error) {
	// Partial unique index: (rated_by_user_id, order_id) WHERE rating_type = 'ORDER'
	const q = `
		INSERT INTO public.ratings
			(rating_type, rated_by_user_id, order_id, plant_quality_rating, communication_rating, overall_experience_rating, would_buy_again, comment, updated_at)
		VALUES ('ORDER', $1, $2, $3, $4, $5, $6, NULLIF($7, ''), NOW())
		ON CONFLICT (rated_by_user_id, order_id) WHERE rating_type = 'ORDER' AND order_id IS NOT NULL
		DO UPDATE SET
			plant_quality_rating      = EXCLUDED.plant_quality_rating,
			communication_rating      = EXCLUDED.communication_rating,
			overall_experience_rating = EXCLUDED.overall_experience_rating,
			would_buy_again           = EXCLUDED.would_buy_again,
			comment                   = EXCLUDED.comment,
			updated_at                = NOW()
		RETURNING rating_id
	`
	var id int64
	if err := r.db.QueryRowContext(ctx, q, userID, orderID, req.PlantQuality, req.Communication, req.OverallExperience, req.WouldBuyAgain, req.Comment).Scan(&id); err != nil {
		return nil, err
	}
	return r.findByID(ctx, id)
}

func (r *PostgresRepository) List(ctx context.Context, input ListRatingsRequest) ([]Rating, error) {
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PerPage <= 0 {
		input.PerPage = 50
	}
	args := []any{}
	where := "1 = 1"
	if input.RatingType != "" {
		args = append(args, input.RatingType)
		where = fmt.Sprintf("rating_type = $%d", len(args))
	}
	offset := (input.Page - 1) * input.PerPage
	args = append(args, input.PerPage, offset)
	n := len(args)
	query := fmt.Sprintf(
		"SELECT %s FROM public.ratings WHERE %s ORDER BY rating_id DESC LIMIT $%d OFFSET $%d",
		selectCols(), where, n-1, n,
	)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []Rating
	for rows.Next() {
		rt, err := scanRating(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, rt)
	}
	return results, rows.Err()
}

func (r *PostgresRepository) GetApp(ctx context.Context, userID int64) (*Rating, error) {
	row := r.db.QueryRowContext(ctx,
		"SELECT "+selectCols()+" FROM public.ratings WHERE rating_type = 'APP' AND rated_by_user_id = $1 LIMIT 1",
		userID)
	rt, err := scanRating(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &rt, err
}

func (r *PostgresRepository) GetTrip(ctx context.Context, userID int64, dispatchID int64) (*Rating, error) {
	row := r.db.QueryRowContext(ctx,
		"SELECT "+selectCols()+" FROM public.ratings WHERE rating_type = 'TRIP' AND rated_by_user_id = $1 AND dispatch_id = $2 LIMIT 1",
		userID, dispatchID)
	rt, err := scanRating(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &rt, err
}

func (r *PostgresRepository) GetOrder(ctx context.Context, userID int64, orderID int64) (*Rating, error) {
	row := r.db.QueryRowContext(ctx,
		"SELECT "+selectCols()+" FROM public.ratings WHERE rating_type = 'ORDER' AND rated_by_user_id = $1 AND order_id = $2 LIMIT 1",
		userID, orderID)
	rt, err := scanRating(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &rt, err
}

func (r *PostgresRepository) findByID(ctx context.Context, id int64) (*Rating, error) {
	row := r.db.QueryRowContext(ctx,
		"SELECT "+selectCols()+" FROM public.ratings WHERE rating_id = $1", id)
	rt, err := scanRating(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &rt, err
}

func selectCols() string {
	return `rating_id, rating_type, rated_by_user_id,
		order_id, dispatch_id,
		overall_rating, would_recommend,
		driver_behaviour_rating, on_time_delivery_rating, plant_condition_rating,
		plant_quality_rating, communication_rating, overall_experience_rating, would_buy_again,
		comment, created_at, updated_at`
}

func scanRating(row interface{ Scan(dest ...any) error }) (Rating, error) {
	var rt Rating
	var orderID, dispatchID sql.NullInt64
	var overallRating, driverBeh, onTime, plantCond sql.NullInt16
	var plantQual, commRating, overallExp sql.NullInt16
	var wouldRec, wouldBuy sql.NullBool
	var comment sql.NullString
	if err := row.Scan(
		&rt.ID, &rt.RatingType, &rt.RatedByUserID,
		&orderID, &dispatchID,
		&overallRating, &wouldRec,
		&driverBeh, &onTime, &plantCond,
		&plantQual, &commRating, &overallExp, &wouldBuy,
		&comment, &rt.CreatedAt, &rt.UpdatedAt,
	); err != nil {
		return Rating{}, err
	}
	if orderID.Valid {
		v := orderID.Int64
		rt.OrderID = &v
	}
	if dispatchID.Valid {
		v := dispatchID.Int64
		rt.DispatchID = &v
	}
	if overallRating.Valid {
		v := int(overallRating.Int16)
		rt.OverallRating = &v
	}
	if wouldRec.Valid {
		rt.WouldRecommend = &wouldRec.Bool
	}
	if driverBeh.Valid {
		v := int(driverBeh.Int16)
		rt.DriverBehaviour = &v
	}
	if onTime.Valid {
		v := int(onTime.Int16)
		rt.OnTimeDelivery = &v
	}
	if plantCond.Valid {
		v := int(plantCond.Int16)
		rt.PlantCondition = &v
	}
	if plantQual.Valid {
		v := int(plantQual.Int16)
		rt.PlantQuality = &v
	}
	if commRating.Valid {
		v := int(commRating.Int16)
		rt.Communication = &v
	}
	if overallExp.Valid {
		v := int(overallExp.Int16)
		rt.OverallExperience = &v
	}
	if wouldBuy.Valid {
		rt.WouldBuyAgain = &wouldBuy.Bool
	}
	if comment.Valid {
		rt.Comment = &comment.String
	}
	return rt, nil
}
