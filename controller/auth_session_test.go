package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAuthLogoutRejectsRefreshCookieSessionMismatch(t *testing.T) {
	previousDB := model.DB
	previousRedis := common.RedisEnabled
	previousSecret := common.SessionSecret
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}))
	model.DB = db
	common.RedisEnabled = false
	common.SessionSecret = "auth-logout-mismatch-test-secret"
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedis
		common.SessionSecret = previousSecret
	})

	user := &model.User{
		Username: "logout-mismatch-user", Password: "unused", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(user).Error)
	sessionA, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "agent-a")
	require.NoError(t, err)
	sessionB, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "agent-b")
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/auth/logout", nil)
	c.Request.Header.Set("Authorization", "Bearer "+sessionA.AccessToken)
	c.Request.Header.Set("X-Auth-Session", sessionA.Session.SID)
	c.Request.AddCookie(&http.Cookie{Name: service.RefreshCookieName, Value: sessionB.RefreshToken})

	AuthLogout(c)

	assert.Equal(t, http.StatusConflict, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "AUTH_SESSION_MISMATCH", response.Code)
	for _, sid := range []string{sessionA.Session.SID, sessionB.Session.SID} {
		stored, err := model.GetUserSessionBySID(sid)
		require.NoError(t, err)
		assert.Equal(t, model.UserSessionStatusActive, stored.Status)
	}
}

func TestWriteAuthSessionErrorMapsSessionGrowthLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "active session limit",
			err:            model.ErrUserSessionLimit,
			expectedStatus: http.StatusConflict,
			expectedCode:   "AUTH_SESSION_LIMIT",
		},
		{
			name:           "issuance limit",
			err:            model.ErrUserSessionIssuanceLimit,
			expectedStatus: http.StatusTooManyRequests,
			expectedCode:   "AUTH_SESSION_ISSUANCE_LIMIT",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			writeAuthSessionError(c, test.err)

			assert.Equal(t, test.expectedStatus, recorder.Code)
			var response struct {
				Success bool   `json:"success"`
				Code    string `json:"code"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Equal(t, test.expectedCode, response.Code)
		})
	}
}

func TestSessionLimitDoesNotRecordRejectedLoginAsSuccessful(t *testing.T) {
	previousDB := model.DB
	previousRedis := common.RedisEnabled
	previousActiveLimit := common.UserSessionActiveLimit
	previousIssuanceLimit := common.UserSessionIssuanceLimit
	previousIssuanceWindow := common.UserSessionIssuanceWindowSeconds
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.TwoFA{}, &model.PasskeyCredential{}))
	model.DB = db
	common.RedisEnabled = false
	common.UserSessionActiveLimit = 1
	common.UserSessionIssuanceLimit = 100
	common.UserSessionIssuanceWindowSeconds = int64(common.DefaultUserSessionIssuanceWindowSeconds)
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedis
		common.UserSessionActiveLimit = previousActiveLimit
		common.UserSessionIssuanceLimit = previousIssuanceLimit
		common.UserSessionIssuanceWindowSeconds = previousIssuanceWindow
	})

	const previousLastLoginAt = int64(123)
	user := &model.User{
		Username: "rejected-login-audit-user", Password: "unused", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, LastLoginAt: previousLastLoginAt,
	}
	require.NoError(t, db.Create(user).Error)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.UserSession{
		SID: "existing-active-session", UserID: user.Id, Version: 1, UserAuthVersion: user.AuthVersion,
		Status: model.UserSessionStatusActive, RefreshHash: "hash", LoginMethod: "password",
		CreatedAt: now, LastActiveAt: now, ExpiresAt: now + 3600,
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/login", nil)
	setupLogin(user, nil, c)

	assert.Equal(t, http.StatusConflict, recorder.Code)
	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	assert.Equal(t, previousLastLoginAt, stored.LastLoginAt)
}

// These tests exercise the real public registration handler using the production
// no-gift policy. They never use the running instance or an upstream API.
func TestPublicRegistrationCreatesZeroBalanceUserWhoCanLogin(t *testing.T) {
	setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}))
	oldRegister, oldPasswordRegister := common.RegisterEnabled, common.PasswordRegisterEnabled
	oldLogin, oldEmail := common.PasswordLoginEnabled, common.EmailVerificationEnabled
	oldQuota, oldInviter, oldInvitee := common.QuotaForNewUser, common.QuotaForInviter, common.QuotaForInvitee
	oldDefaultToken := constant.GenerateDefaultToken
	common.RegisterEnabled, common.PasswordRegisterEnabled, common.PasswordLoginEnabled = true, true, true
	common.EmailVerificationEnabled, constant.GenerateDefaultToken = false, false
	common.QuotaForNewUser, common.QuotaForInviter, common.QuotaForInvitee = 0, 0, 0
	t.Cleanup(func() {
		common.RegisterEnabled, common.PasswordRegisterEnabled = oldRegister, oldPasswordRegister
		common.PasswordLoginEnabled, common.EmailVerificationEnabled = oldLogin, oldEmail
		common.QuotaForNewUser, common.QuotaForInviter, common.QuotaForInvitee = oldQuota, oldInviter, oldInvitee
		constant.GenerateDefaultToken = oldDefaultToken
	})

	const username = "public-creator"
	const password = "A new creator account 2026!"
	body, err := common.Marshal(map[string]any{
		"username": username, "password": password,
		"email": "unverified@example.com", "role": common.RoleRootUser,
		"quota": 1000000000, "group": "vip", "status": common.UserStatusDisabled,
	})
	require.NoError(t, err)
	router := gin.New()
	router.POST("/api/user/register", Register)
	router.POST("/api/user/login", Login)
	registered := httptest.NewRecorder()
	router.ServeHTTP(registered, httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(string(body))))
	var registration struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(registered.Body.Bytes(), &registration))
	require.True(t, registration.Success, registered.Body.String())

	var user model.User
	require.NoError(t, model.DB.Where("username = ?", username).First(&user).Error)
	assert.Equal(t, common.RoleCommonUser, user.Role)
	assert.Equal(t, common.UserStatusEnabled, user.Status)
	assert.Equal(t, "default", user.Group)
	assert.Zero(t, user.Quota)
	assert.Empty(t, user.Email, "unverified emails must not become recovery credentials")
	assert.True(t, strings.HasPrefix(user.Password, "$argon2id$"))
	assert.NotEqual(t, password, user.Password)

	repeated := httptest.NewRecorder()
	router.ServeHTTP(repeated, httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(string(body))))
	require.NoError(t, common.Unmarshal(repeated.Body.Bytes(), &registration))
	assert.False(t, registration.Success, "a repeated registration must not create another user")
	var userCount int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", username).Count(&userCount).Error)
	assert.EqualValues(t, 1, userCount)
	var tokenCount int64
	require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ?", user.Id).Count(&tokenCount).Error)
	assert.Zero(t, tokenCount)

	loginBody, err := common.Marshal(map[string]string{"username": username, "password": password})
	require.NoError(t, err)
	loggedIn := httptest.NewRecorder()
	router.ServeHTTP(loggedIn, httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader(string(loginBody))))
	var login struct {
		Success bool               `json:"success"`
		Data    service.AuthBundle `json:"data"`
	}
	require.NoError(t, common.Unmarshal(loggedIn.Body.Bytes(), &login))
	require.True(t, login.Success, loggedIn.Body.String())
	assert.NotEmpty(t, login.Data.AccessToken)
	assert.NotEmpty(t, loggedIn.Header().Values("Set-Cookie"))
	assert.NotContains(t, loggedIn.Body.String(), password)
	assert.NotContains(t, loggedIn.Body.String(), user.Password)
}

func TestPublicRegistrationRejectsClosedAndUnverifiedRequests(t *testing.T) {
	setupSecurityEnrollmentTest(t)
	oldRegister, oldPasswordRegister, oldEmail := common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled
	t.Cleanup(func() {
		common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = oldRegister, oldPasswordRegister, oldEmail
	})
	for _, test := range []struct {
		name              string
		register          bool
		passwordRegister  bool
		emailVerification bool
		password          string
		email             string
		code              string
	}{
		{name: "registration closed", passwordRegister: true, password: "A valid account password"},
		{name: "password registration closed", register: true, password: "A valid account password"},
		{name: "weak password", register: true, passwordRegister: true, password: "short"},
		{name: "email verification missing", register: true, passwordRegister: true, emailVerification: true, password: "A valid account password", email: "creator@example.com"},
		{name: "email verification invalid", register: true, passwordRegister: true, emailVerification: true, password: "A valid account password", email: "creator@example.com", code: "invalid-code"},
	} {
		t.Run(test.name, func(t *testing.T) {
			common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = test.register, test.passwordRegister, test.emailVerification
			body, err := common.Marshal(map[string]string{"username": "rejected-creator", "password": test.password, "email": test.email, "verification_code": test.code})
			require.NoError(t, err)
			response := securityEnrollmentRequest(http.MethodPost, "/api/user/register", string(body), "", service.AuthIdentity{}, Register)
			var result struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			assert.False(t, result.Success)
			assert.Empty(t, response.Header().Values("Set-Cookie"))
			var count int64
			require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", "rejected-creator").Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestRegistrationPasswordPolicyRejectsShortNewPasswordsButPreservesLogin(t *testing.T) {
	setupSecurityEnrollmentTest(t)
	oldMinimum := common.RegistrationPasswordMinLength
	oldRegister, oldPasswordRegister, oldLogin, oldEmail := common.RegisterEnabled, common.PasswordRegisterEnabled, common.PasswordLoginEnabled, common.EmailVerificationEnabled
	oldToken, oldQuota := constant.GenerateDefaultToken, common.QuotaForNewUser
	common.RegisterEnabled, common.PasswordRegisterEnabled, common.PasswordLoginEnabled = true, true, true
	common.EmailVerificationEnabled, constant.GenerateDefaultToken, common.QuotaForNewUser = false, false, 0
	t.Setenv("REGISTRATION_PASSWORD_MIN_LENGTH", "15")
	require.NoError(t, common.InitRegistrationPasswordSettings())
	t.Cleanup(func() {
		common.RegistrationPasswordMinLength = oldMinimum
		common.RegisterEnabled, common.PasswordRegisterEnabled, common.PasswordLoginEnabled, common.EmailVerificationEnabled = oldRegister, oldPasswordRegister, oldLogin, oldEmail
		constant.GenerateDefaultToken, common.QuotaForNewUser = oldToken, oldQuota
	})
	for i, test := range []struct {
		password string
		accepted bool
	}{
		{password: "12345678", accepted: false},
		{password: strings.Repeat("a", 14), accepted: false},
		{password: "A new password!", accepted: true},
		{password: strings.Repeat("创", 15), accepted: true},
	} {
		body, err := common.Marshal(map[string]string{"username": fmt.Sprintf("password-case-%d", i), "password": test.password})
		require.NoError(t, err)
		response := securityEnrollmentRequest(http.MethodPost, "/api/user/register", string(body), "", service.AuthIdentity{}, Register)
		var result struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.Equal(t, test.accepted, result.Success, response.Body.String())
	}

	const legacyPassword = "old-pass"
	hash, err := common.HashAccountPassword(legacyPassword)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.User{Username: "existing-creator", AffCode: "existing-creator", Password: hash, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1}).Error)
	body, err := common.Marshal(map[string]string{"username": "existing-creator", "password": legacyPassword})
	require.NoError(t, err)
	response := securityEnrollmentRequest(http.MethodPost, "/api/user/login", string(body), "", service.AuthIdentity{}, Login)
	var result struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.True(t, result.Success, response.Body.String())
	assert.NotEmpty(t, response.Header().Values("Set-Cookie"))
}

func TestRegistrationPasswordConfigurationBounds(t *testing.T) {
	oldMinimum := common.RegistrationPasswordMinLength
	t.Cleanup(func() { common.RegistrationPasswordMinLength = oldMinimum })
	for _, test := range []struct {
		value   string
		want    int
		invalid bool
	}{
		{value: "", want: 8}, {value: "15", want: 15}, {value: "128", want: 128},
		{value: "7", invalid: true}, {value: "129", invalid: true}, {value: "15.5", invalid: true}, {value: "invalid", invalid: true},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("REGISTRATION_PASSWORD_MIN_LENGTH", test.value)
			err := common.InitRegistrationPasswordSettings()
			if test.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, common.RegistrationPasswordMinLength)
		})
	}
}
