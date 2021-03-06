package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dgrijalva/jwt-go"
	"github.com/netlify/gotrue/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type AdminTestSuite struct {
	suite.Suite
	User *models.User
	API  *API
}

func (ts *AdminTestSuite) SetupTest() {
	api, err := NewAPIFromConfigFile("config.test.json", "v1")
	require.NoError(ts.T(), err)
	ts.API = api
}

// TestAdminUsersUnauthorized tests API /admin/users route without authentication
func (ts *AdminTestSuite) TestAdminUsersUnauthorized() {
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	w := httptest.NewRecorder()

	ts.API.handler.ServeHTTP(w, req)
	assert.Equal(ts.T(), w.Code, http.StatusUnauthorized)
}

func (ts *AdminTestSuite) makeSuperAdmin(req *http.Request, email string) {
	// Cleanup existing user, if they already exist
	if u, _ := ts.API.db.FindUserByEmailAndAudience(email, ts.API.config.JWT.Aud); u != nil {
		require.NoError(ts.T(), ts.API.db.DeleteUser(u), "Error deleting user")
	}

	u, err := models.NewUser(email, "test", ts.API.config.JWT.Aud, nil)
	require.NoError(ts.T(), err, "Error making new user")

	u.IsSuperAdmin = true
	require.NoError(ts.T(), ts.API.db.CreateUser(u), "Error creating user")

	token, err := ts.API.generateAccessToken(u)
	require.NoError(ts.T(), err, "Error generating access token")

	_, err = jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		assert.Equal(ts.T(), token.Method.Alg(), jwt.SigningMethodHS256.Name)
		return []byte(ts.API.config.JWT.Secret), nil
	})
	require.NoError(ts.T(), err, "Error parsing token")

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
}

// TestAdminUsers tests API /admin/users route
func (ts *AdminTestSuite) TestAdminUsers() {
	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)

	// Setup response recorder with super admin privileges
	ts.makeSuperAdmin(req, "test@example.com")

	ts.API.handler.ServeHTTP(w, req)

	data := make(map[string]interface{})
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))

	assert.Equal(ts.T(), w.Code, http.StatusOK)
	for _, user := range data["users"].([]interface{}) {
		assert.NotEmpty(ts.T(), user)
	}
}

// TestAdminUserCreate tests API /admin/user route (POST)
func (ts *AdminTestSuite) TestAdminUserCreate() {
	var buffer bytes.Buffer
	require.NoError(ts.T(), json.NewEncoder(&buffer).Encode(map[string]interface{}{
		"email":    "test1@example.com",
		"password": "test1",
	}))

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/user", &buffer)

	// Setup response recorder with super admin privileges
	ts.makeSuperAdmin(req, "test@example.com")

	ts.API.handler.ServeHTTP(w, req)

	assert.Equal(ts.T(), w.Code, http.StatusOK)

	u, err := ts.API.db.FindUserByEmailAndAudience("test1@example.com", ts.API.config.JWT.Aud)
	require.NoError(ts.T(), err)

	data := make(map[string]interface{})
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))

	assert.Equal(ts.T(), data["email"], u.Email)
}

// TestAdminUserGet tests API /admin/user route (GET)
func (ts *AdminTestSuite) TestAdminUserGet() {
	u, err := models.NewUser("test1@example.com", "test", ts.API.config.JWT.Aud, nil)
	require.NoError(ts.T(), err, "Error making new user")
	require.NoError(ts.T(), ts.API.db.CreateUser(u), "Error creating user")

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/user?email=test1@example.com&aud=%s", ts.API.config.JWT.Aud), nil)

	// Setup response recorder with super admin privileges
	ts.makeSuperAdmin(req, "test@example.com")

	ts.API.handler.ServeHTTP(w, req)

	assert.Equal(ts.T(), w.Code, http.StatusOK)

	data := make(map[string]interface{})
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))

	assert.Equal(ts.T(), data["email"], "test1@example.com")
}

// TestAdminUserUpdate tests API /admin/user route (UPDATE)
func (ts *AdminTestSuite) TestAdminUserUpdate() {
	var buffer bytes.Buffer
	require.NoError(ts.T(), json.NewEncoder(&buffer).Encode(map[string]interface{}{
		"role": "testing",
		"user": map[string]interface{}{
			"email": "test1@example.com",
			"aud":   ts.API.config.JWT.Aud,
		},
	}))

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/user", &buffer)

	// Setup response recorder with super admin privileges
	ts.makeSuperAdmin(req, "test@example.com")

	ts.API.handler.ServeHTTP(w, req)

	assert.Equal(ts.T(), w.Code, http.StatusOK)

	data := make(map[string]interface{})
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))

	assert.Equal(ts.T(), data["role"], "testing")

	u, err := ts.API.db.FindUserByEmailAndAudience("test1@example.com", ts.API.config.JWT.Aud)
	require.NoError(ts.T(), err)
	assert.Equal(ts.T(), u.Role, "testing")
}

// TestAdminUserDelete tests API /admin/user route (DELETE)
func (ts *AdminTestSuite) TestAdminUserDelete() {
	var buffer bytes.Buffer
	require.NoError(ts.T(), json.NewEncoder(&buffer).Encode(map[string]interface{}{
		"user": map[string]interface{}{
			"email": "test1@example.com",
			"aud":   ts.API.config.JWT.Aud,
		},
	}))

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/admin/user", &buffer)

	// Setup response recorder with super admin privileges
	ts.makeSuperAdmin(req, "test@example.com")

	ts.API.handler.ServeHTTP(w, req)

	assert.Equal(ts.T(), w.Code, http.StatusOK)
}

func TestAdmin(t *testing.T) {
	suite.Run(t, new(AdminTestSuite))
}
