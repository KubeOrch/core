package models

// LoginRequest represents the login request payload
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// RegisterRequest represents the registration request payload
type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Name     string `json:"name" binding:"required"`
}

// AuthResponse represents the authentication response
type AuthResponse struct {
	Token string       `json:"token"`
	User  UserResponse `json:"user"`
}

// UserResponse represents the user data in API responses
type UserResponse struct {
	ID    string   `json:"id"`
	Email string   `json:"email"`
	Name  string   `json:"name"`
	Role  UserRole `json:"role"`
}
