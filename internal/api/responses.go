package api

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// SuccessResponse represents a generic success response.
type SuccessResponse struct {
	Success bool `json:"success"`
}
