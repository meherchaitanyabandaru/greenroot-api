package users

import "time"

type UpdateProfileRequest struct {
	FirstName       string  `json:"first_name"`
	LastName        *string `json:"last_name"`
	Email           *string `json:"email"`
	ProfileImageURL *string `json:"profile_image_url"`
	Gender          *string `json:"gender,omitempty"`
}

type CompleteOnboardingRequest struct {
	InitialActivity string `json:"initial_activity"`
}

type CreateAddressRequest struct {
	AddressType         *string    `json:"address_type"`
	ContactName         *string    `json:"contact_name"`
	ContactMobile       *string    `json:"contact_mobile"`
	AddressLine1        string     `json:"address_line1"`
	AddressLine2        *string    `json:"address_line2"`
	City                *string    `json:"city"`
	State               *string    `json:"state"`
	Country             *string    `json:"country"`
	PostalCode          *string    `json:"postal_code"`
	Latitude            *float64   `json:"latitude"`
	Longitude           *float64   `json:"longitude"`
	GPSAccuracyM        *float64   `json:"gps_accuracy_meters"`
	Landmark            *string    `json:"landmark"`
	LocationSource      *string    `json:"location_source"`
	LocationConfirmedBy *int64     `json:"location_confirmed_by,omitempty"`
	LocationConfirmedAt *time.Time `json:"location_confirmed_at,omitempty"`
	IsDefault           bool       `json:"is_default"`
}

type UpdateAddressRequest = CreateAddressRequest

type UserResponse struct {
	User User `json:"user"`
}

type AddressesResponse struct {
	Addresses []Address `json:"addresses"`
}

type AddressResponse struct {
	Address Address `json:"address"`
}

type RolesResponse struct {
	Roles []Role `json:"roles"`
}

type SessionsResponse struct {
	Sessions []Session `json:"sessions"`
}

type DeleteAddressResponse struct {
	Message string `json:"message"`
}
