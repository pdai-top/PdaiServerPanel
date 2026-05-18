package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pdai/pdai/internal/auth"
	"github.com/pdai/pdai/internal/config"
	"github.com/pdai/pdai/internal/model"
	"gorm.io/gorm"
)

// AuthHandler manages authentication endpoints for the single-admin mode.
type AuthHandler struct {
	db  *gorm.DB
	cfg *config.Config
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(db *gorm.DB, cfg *config.Config) *AuthHandler {
	return &AuthHandler{db: db, cfg: cfg}
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Altcha   string `json:"altcha" binding:"required"`
}

type setupRequest struct {
	Username string `json:"username" binding:"required,min=3"`
	Password string `json:"password" binding:"required,min=8"`
	Altcha   string `json:"altcha" binding:"required"`
}

type updateAdminRequest struct {
	Username        string `json:"username" binding:"required,min=3"`
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type altchaChallengeResponse struct {
	Salt      string `json:"salt"`
	Challenge string `json:"challenge"`
	MaxNumber int    `json:"maxnumber"`
	Algorithm string `json:"algorithm"`
	Signature string `json:"signature"`
}

// AltchaChallenge returns a proof-of-work challenge for login/setup pages.
func (h *AuthHandler) AltchaChallenge(c *gin.Context) {
	resp, err := buildAltchaChallenge(h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create challenge"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// Setup creates the initial admin user (only works when no users exist).
func (h *AuthHandler) Setup(c *gin.Context) {
	var count int64
	h.db.Model(&model.User{}).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Admin user already exists"})
		return
	}

	var req setupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := verifyAltchaPayload(req.Altcha, h.cfg.JWTSecret); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid verification challenge"})
		return
	}

	hashed, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	user := model.User{
		Username: req.Username,
		Password: hashed,
		Role:     "admin",
	}

	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	token, _ := auth.GenerateToken(user.ID, user.Username, h.cfg.JWTSecret)
	c.JSON(http.StatusOK, gin.H{
		"message": "Admin user created successfully",
		"token":   token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

// Login authenticates the single admin user and returns a JWT token.
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := verifyAltchaPayload(req.Altcha, h.cfg.JWTSecret); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid verification challenge"})
		return
	}

	var user model.User
	if err := h.db.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if !auth.CheckPassword(user.Password, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	token, err := auth.GenerateToken(user.ID, user.Username, h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

// Me returns the current authenticated user info.
func (h *AuthHandler) Me(c *gin.Context) {
	userID, _ := c.Get("user_id")
	username, _ := c.Get("username")
	role := "admin"
	if id, ok := userID.(uint); ok {
		var user model.User
		if err := h.db.Select("role").First(&user, id).Error; err == nil && user.Role != "" {
			role = user.Role
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"id":       userID,
		"username": username,
		"role":     role,
	})
}

// UpdateAdminProfile updates the single administrator login name and password.
func (h *AuthHandler) UpdateAdminProfile(c *gin.Context) {
	userIDValue, ok := c.Get("user_id")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	userID, ok := userIDValue.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req updateAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user model.User
	if err := h.db.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Admin user not found"})
		return
	}

	updates := map[string]interface{}{"username": req.Username}
	if req.NewPassword != "" {
		if len(req.NewPassword) < 8 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "New password must be at least 8 characters"})
			return
		}
		if req.CurrentPassword == "" || !auth.CheckPassword(user.Password, req.CurrentPassword) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Current password is incorrect"})
			return
		}
		hashed, err := auth.HashPassword(req.NewPassword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}
		updates["password"] = hashed
	}

	if err := h.db.Model(&user).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update admin profile"})
		return
	}

	token, err := auth.GenerateToken(user.ID, req.Username, h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	h.audit(c, "admin.update_profile", "updated admin profile")
	c.JSON(http.StatusOK, gin.H{
		"message": "Admin profile updated successfully",
		"token":   token,
		"user": gin.H{
			"id":       user.ID,
			"username": req.Username,
			"role":     user.Role,
		},
	})
}

// NeedSetup checks if initial setup is required.
func (h *AuthHandler) NeedSetup(c *gin.Context) {
	var count int64
	h.db.Model(&model.User{}).Count(&count)
	c.JSON(http.StatusOK, gin.H{"need_setup": count == 0})
}

func (h *AuthHandler) audit(c *gin.Context, action, detail string) {

	if uid, ok := c.Get("user_id"); ok {

		uname, _ := c.Get("username")

		WriteAuditLog(h.db, uid.(uint), fmt.Sprint(uname), action, "user", fmt.Sprint(uid), detail, c.ClientIP())

	}

}

func buildAltchaChallenge(secret string) (*altchaChallengeResponse, error) {

	const maxNumber = 50000

	saltBytes := make([]byte, 16)

	if _, err := rand.Read(saltBytes); err != nil {

		return nil, err

	}

	salt := hex.EncodeToString(saltBytes)

	n, err := rand.Int(rand.Reader, big.NewInt(maxNumber+1))

	if err != nil {

		return nil, err

	}

	number := int(n.Int64())

	challenge := altchaHash(salt, number)

	signature := signAltcha(secret, salt, challenge, maxNumber)

	return &altchaChallengeResponse{

		Salt: salt,

		Challenge: challenge,

		MaxNumber: maxNumber,

		Algorithm: "SHA-256",

		Signature: signature,
	}, nil

}

type altchaPayload struct {
	Algorithm string `json:"algorithm"`

	Challenge string `json:"challenge"`

	Number int `json:"number"`

	Salt string `json:"salt"`

	Signature string `json:"signature"`
}

func verifyAltchaPayload(encoded, secret string) error {

	data, err := base64.StdEncoding.DecodeString(encoded)

	if err != nil {

		return err

	}

	var payload altchaPayload

	if err := json.Unmarshal(data, &payload); err != nil {

		return err

	}

	if !strings.EqualFold(payload.Algorithm, "SHA-256") || payload.Salt == "" || payload.Challenge == "" || payload.Number < 0 || payload.Number > 50000 {

		return fmt.Errorf("invalid altcha payload")

	}

	if !hmac.Equal([]byte(payload.Challenge), []byte(altchaHash(payload.Salt, payload.Number))) {

		return fmt.Errorf("invalid altcha answer")

	}

	if !hmac.Equal([]byte(payload.Signature), []byte(signAltcha(secret, payload.Salt, payload.Challenge, 50000))) {

		return fmt.Errorf("invalid altcha signature")

	}

	return nil

}

func altchaHash(salt string, number int) string {

	sum := sha256.Sum256([]byte(salt + strconv.Itoa(number)))

	return hex.EncodeToString(sum[:])

}

func signAltcha(secret, salt, challenge string, maxNumber int) string {

	mac := hmac.New(sha256.New, []byte(secret))

	mac.Write([]byte(salt))

	mac.Write([]byte("|"))

	mac.Write([]byte(challenge))

	mac.Write([]byte("|"))

	mac.Write([]byte(strconv.Itoa(maxNumber)))

	return hex.EncodeToString(mac.Sum(nil))

}
