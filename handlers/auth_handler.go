package handlers

import (
	"net/http"
	"sync"

	"github.com/KubeOrchestra/core/models"
	"github.com/KubeOrchestra/core/services"
	"github.com/gin-gonic/gin"
)

type UserStore struct {
	users    map[string]models.User
	usersMux sync.RWMutex
	nextID   uint
	idMux    sync.Mutex
}

func NewUserStore() *UserStore {
	return &UserStore{
		users:  make(map[string]models.User),
		nextID: 1,
	}
}

var userStore = NewUserStore()

func RegisterHandler(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request data",
		})
		return
	}

	userStore.usersMux.Lock()
	defer userStore.usersMux.Unlock()

	if _, exists := userStore.users[req.Email]; exists {
		c.JSON(http.StatusConflict, gin.H{
			"error": "User already exists",
		})
		return
	}

	hashedPassword, err := services.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Registration failed",
		})
		return
	}

	userStore.idMux.Lock()
	userID := userStore.nextID
	userStore.nextID++
	userStore.idMux.Unlock()

	user := models.User{
		ID:       userID,
		Email:    req.Email,
		Name:     req.Name,
		Password: hashedPassword,
	}

	userStore.users[req.Email] = user

	token, err := services.GenerateJWTToken(user.ID, user.Email)
	if err != nil {
		delete(userStore.users, req.Email)
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

	userStore.usersMux.RLock()
	user, exists := userStore.users[req.Email]
	userStore.usersMux.RUnlock()

	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid email or password",
		})
		return
	}

	if !services.CheckPasswordHash(req.Password, user.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid email or password",
		})
		return
	}

	token, err := services.GenerateJWTToken(user.ID, user.Email)
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
