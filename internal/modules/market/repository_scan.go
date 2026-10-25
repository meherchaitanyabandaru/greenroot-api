// Row scanners for the market repository.

package market

import (
	"database/sql"
	"encoding/json"
	"errors"
)

func scanMessage(row rowScanner) (Message, error) {
	var m Message
	err := row.Scan(&m.ID, &m.EnquiryID, &m.SentByUserID, &m.SentByNurseryID,
		&m.NurseryName, &m.SenderName, &m.Type, &m.Body,
		&m.OfferPrice, &m.OfferQuantity, &m.OfferStatus, &m.CreatedAt,
		&m.SenderAvatarURL, &m.Receipt)
	return m, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAd(row rowScanner) (Ad, error) {
	return scanAdRow(row)
}

func scanAdRow(row rowScanner) (Ad, error) {
	var a Ad
	var photosRaw []byte
	var plantID sql.NullInt64
	var nurseryMobile, categoryName, description, sizeDesc, priceUnit sql.NullString
	var pickupAddress, pickupLandmark, pickupSource sql.NullString
	var pricePerUnit, pickupLat, pickupLon, pickupAccuracy sql.NullFloat64
	var quantity sql.NullInt64
	var pickupConfirmedBy sql.NullInt64
	var pickupConfirmedAt, expiresAt, publishedAt, pausedAt, expiredAt, archivedAt sql.NullTime

	if err := row.Scan(
		&a.ID, &a.Code, &a.AdType, &a.NeededBy, &a.NurseryID, &a.NurseryName, &a.NurseryVerified, &nurseryMobile,
		&a.CreatedByUserID, &plantID, &a.PlantName, &categoryName,
		&a.Title, &description, &quantity, &sizeDesc,
		&pricePerUnit, &priceUnit, &photosRaw,
		&pickupAddress, &pickupLandmark, &pickupLat, &pickupLon,
		&pickupAccuracy, &pickupSource, &pickupConfirmedBy, &pickupConfirmedAt,
		&a.RoadAccessible, &a.TruckAccessible, &a.AccessInstructions,
		&a.Status, &a.ViewCount, &a.SaveCount, &a.EnquiryCount,
		&expiresAt, &publishedAt, &pausedAt, &expiredAt, &archivedAt, &a.ModerationNote,
		&a.PostedByName, &a.PostedByPhoto,
		&a.CreatedAt, &a.UpdatedAt, &a.DistanceKM,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Ad{}, ErrNotFound
		}
		return Ad{}, err
	}
	if nurseryMobile.Valid {
		a.NurseryMobile = &nurseryMobile.String
	}
	if plantID.Valid {
		a.PlantID = &plantID.Int64
	}
	if categoryName.Valid {
		a.CategoryName = &categoryName.String
	}
	if description.Valid {
		a.Description = &description.String
	}
	if quantity.Valid {
		v := int(quantity.Int64)
		a.Quantity = &v
	}
	if sizeDesc.Valid {
		a.SizeDescription = &sizeDesc.String
	}
	if pricePerUnit.Valid {
		a.PricePerUnit = &pricePerUnit.Float64
	}
	if priceUnit.Valid {
		a.PriceUnit = &priceUnit.String
	}
	if pickupAddress.Valid {
		a.PickupAddress = &pickupAddress.String
	}
	if pickupLandmark.Valid {
		a.PickupLandmark = &pickupLandmark.String
	}
	if pickupLat.Valid {
		a.PickupLatitude = &pickupLat.Float64
	}
	if pickupLon.Valid {
		a.PickupLongitude = &pickupLon.Float64
	}
	if pickupAccuracy.Valid {
		a.PickupGPSAccuracyM = &pickupAccuracy.Float64
	}
	if pickupSource.Valid {
		a.PickupLocationSource = &pickupSource.String
	}
	if pickupConfirmedBy.Valid {
		a.PickupConfirmedBy = &pickupConfirmedBy.Int64
	}
	if pickupConfirmedAt.Valid {
		a.PickupConfirmedAt = &pickupConfirmedAt.Time
	}
	if expiresAt.Valid {
		a.ExpiresAt = &expiresAt.Time
	}
	if publishedAt.Valid {
		a.PublishedAt = &publishedAt.Time
	}
	if pausedAt.Valid {
		a.PausedAt = &pausedAt.Time
	}
	if expiredAt.Valid {
		a.ExpiredAt = &expiredAt.Time
	}
	if archivedAt.Valid {
		a.ArchivedAt = &archivedAt.Time
	}
	if err := json.Unmarshal(photosRaw, &a.Photos); err != nil {
		a.Photos = []string{}
	}
	if a.Photos == nil {
		a.Photos = []string{}
	}
	return a, nil
}

func scanEnquiry(row rowScanner) (Enquiry, error) {
	return scanEnquiryRow(row)
}

func scanEnquiryRow(row rowScanner) (Enquiry, error) {
	var e Enquiry
	var quantityNeeded sql.NullInt64
	var viewedAt, repliedAt sql.NullTime

	if err := row.Scan(
		&e.ID, &e.Code,
		&e.AdID, &e.AdTitle, &e.AdType, &e.AdStatus, &e.AdPricePerUnit, &e.AdPriceUnit, &e.AdQuantity, &e.AdPhoto,
		&e.AdNurseryID, &e.AdNurseryName,
		&e.EnquiringNurseryID, &e.EnquiryNurseryName,
		&e.CreatedByUserID, &e.Message, &quantityNeeded,
		&e.Status, &e.AgreedPricePerUnit, &e.AgreedQuantity, &e.AgreedAt,
		&viewedAt, &repliedAt,
		&e.CreatedAt, &e.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Enquiry{}, ErrNotFound
		}
		return Enquiry{}, err
	}
	if quantityNeeded.Valid {
		v := int(quantityNeeded.Int64)
		e.QuantityNeeded = &v
	}
	if viewedAt.Valid {
		e.ViewedAt = &viewedAt.Time
	}
	if repliedAt.Valid {
		e.RepliedAt = &repliedAt.Time
	}
	e.Messages = []Message{}
	return e, nil
}

func photosJSON(photos []string) []byte {
	if photos == nil {
		photos = []string{}
	}
	b, _ := json.Marshal(photos)
	return b
}
