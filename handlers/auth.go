package handlers

import (
	"net/http"
	"sync"

	"github.com/KubeOrchestra/core/models"
	"github.com/KubeOrchestra/core/utils"
	"github.com/gin-gonic/gin"
)

var (
	users    = make(map[string]models.User)
	usersMux = sync.RWMutex{}
	nextID   = uint(1)
	idMux    = sync.Mutex{}
)

func RegisterHandler(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request data",
		})
		return
	}

	usersMux.RLock()
	_, exists := users[req.Email]
	usersMux.RUnlock()

	if exists {
		c.JSON(http.StatusConflict, gin.H{
			"error": "User already exists",
		})
		return
	}

	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Registration failed",
		})
		return
	}

	idMux.Lock()
	userID := nextID
	nextID++
	idMux.Unlock()

	user := models.User{
		ID:       userID,
		Email:    req.Email,
		Name:     req.Name,
		Password: hashedPassword,
	}

	usersMux.Lock()
	users[req.Email] = user
	usersMux.Unlock()

	token, err := utils.GenerateJWTToken(user.ID, user.Email)
	if err != nil {
		usersMux.Lock()
		delete(users, req.Email)
		usersMux.Unlock()
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Registration failed",
		})
		return
	}

	c.JSON(http.StatusCreated, models.AuthResponse{
		Token: token,
		User:  user,
	})
}

func LoginHandler(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request data",
		})
		return
	}

	usersMux.RLock()
	user, exists := users[req.Email]
	usersMux.RUnlock()

	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid email or password",
		})
		return
	}

	if !utils.CheckPasswordHash(req.Password, user.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid email or password",
		})
		return
	}

	token, err := utils.GenerateJWTToken(user.ID, user.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Login failed",
		})
		return
	}

	c.JSON(http.StatusOK, models.AuthResponse{
		Token: token,
		User:  user,
	})
}
