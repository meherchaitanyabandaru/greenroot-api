package auth

type SendOTPRequest struct {
	Mobile string `json:"mobile"`
}

type VerifyOTPRequest struct {
	Mobile     string `json:"mobile"`
	OTP        string `json:"otp"`
	DeviceType string `json:"device_type,omitempty"`
	OSName     string `json:"os_name,omitempty"`
	AppVersion string `json:"app_version,omitempty"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token,omitempty"`
}

type SendOTPResponse struct {
	Message string `json:"message"`
	MockOTP string `json:"mock_otp,omitempty"`
}

type AuthResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	User         User   `json:"user"`
	IsNewUser    bool   `json:"is_new_user"`
}

type MeResponse struct {
	User User `json:"user"`
}

type LogoutResponse struct {
	Message string `json:"message"`
}
