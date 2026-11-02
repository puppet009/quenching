package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	commonauth "github.com/quenching/go-common/auth"
	"github.com/quenching/user-service/internal/handler"
	"github.com/quenching/user-service/internal/service"
)

// Exercise the same no-provider setup as the MVP server through its public HTTP contract.
func TestMVPRegistrationWithoutVerificationCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		account string
		key     string
	}{
		{name: "phone", account: "13812345678", key: "phone:13812345678"},
		{name: "email", account: "dev@example.com", key: "email:dev@example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			jwt := commonauth.NewManager("registration-test-secret")
			svc := service.NewAuthService(store, jwt, service.WithRegistrationSMSRequired(false))
			r := gin.New()
			handler.NewAuthHandler(svc).RegisterRoutes(r)
			payload := map[string]string{"account": tc.account, "password": "abc12345", "nickname": "MVP user"}
			res := authRequest(t, r, "/api/v1/auth/register", payload)
			if res.Code != http.StatusOK {
				t.Fatalf("register without a verification provider/code: HTTP %d, %s", res.Code, res.Body)
			}
			var registered struct {
				Code int                `json:"code"`
				Data service.AuthResult `json:"data"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &registered); err != nil {
				t.Fatal(err)
			}
			if registered.Code != 0 || registered.Data.User == nil || registered.Data.User.ID <= 0 || registered.Data.Token == nil {
				t.Fatalf("missing successful user/token response: %s", res.Body)
			}
			uid := registered.Data.User.ID
			claims, err := jwt.ParseAccess(registered.Data.Token.AccessToken)
			if err != nil || claims.UID != uid || registered.Data.Token.RefreshToken == "" {
				t.Fatalf("registration must issue usable login tokens: %v", err)
			}
			stored := store.users[tc.key]
			if stored == nil || stored.PasswordHash == payload["password"] || !service.CheckPassword(stored.PasswordHash, payload["password"]) {
				t.Fatal("registration must persist a hashed password")
			}
			if bytes.Contains(res.Body.Bytes(), []byte("password")) {
				t.Fatal("response must not expose a password or its hash")
			}
			duplicate := authRequest(t, r, "/api/v1/auth/register", payload)
			if duplicate.Code != http.StatusConflict || len(store.users) != 1 {
				t.Fatalf("duplicate registration: HTTP %d, %s", duplicate.Code, duplicate.Body)
			}
			login := authRequest(t, r, "/api/v1/auth/login", payload)
			if login.Code != http.StatusOK {
				t.Fatalf("password login after registration: HTTP %d, %s", login.Code, login.Body)
			}
			payload["password"] = "wrong1234"
			wrongPassword := authRequest(t, r, "/api/v1/auth/login", payload)
			if wrongPassword.Code != http.StatusUnauthorized {
				t.Fatalf("incorrect password must be rejected: HTTP %d", wrongPassword.Code)
			}
		})
	}
}

func TestMVPRegistrationStillValidatesAccountAndPassword(t *testing.T) {
	for _, tc := range []struct {
		name     string
		account  string
		password string
		want     error
	}{
		{name: "invalid account", account: "not-an-account", password: "abc12345", want: service.ErrInvalidAccount},
		{name: "short password", account: "13812345678", password: "a123", want: service.ErrPasswordLength},
		{name: "weak password", account: "dev@example.com", password: "abcdefgh", want: service.ErrPasswordWeak},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			svc := service.NewAuthService(store, commonauth.NewManager("secret"), service.WithRegistrationSMSRequired(false))
			_, err := svc.Register(context.Background(), service.RegisterInput{Account: tc.account, Password: tc.password})
			if !errors.Is(err, tc.want) || len(store.users) != 0 {
				t.Fatalf("invalid registration must not create a user: got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRegistrationSMSRequiredRejectsUnverifiedPhone(t *testing.T) {
	for _, mode := range []string{"default", "explicit"} {
		for _, tc := range []struct {
			name    string
			sms     service.SMSService
			code    string
			expired bool
			want    error
		}{
			{name: "missing code", sms: service.NewMockSMSService(), want: service.ErrSMSCodeInvalid},
			{name: "invalid code", sms: service.NewMockSMSService(), code: "not-a-code", want: service.ErrSMSCodeInvalid},
			{name: "missing provider", code: "123456", want: service.ErrSMSSendFailed},
			{name: "expired code", sms: service.NewMockSMSServiceWithTTL(-time.Second), expired: true, want: service.ErrSMSCodeInvalid},
		} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				store := newFakeStore()
				opts := []service.AuthServiceOption{service.WithSMS(tc.sms)}
				if mode == "explicit" {
					opts = append(opts, service.WithRegistrationSMSRequired(true))
				}
				svc := service.NewAuthService(store, commonauth.NewManager("secret"), opts...)
				if tc.expired {
					var err error
					tc.code, err = svc.SendSMSCode(context.Background(), "13812345678", service.SMSPurposeRegister)
					if err != nil {
						t.Fatal(err)
					}
				}
				_, err := svc.Register(context.Background(), service.RegisterInput{Account: "13812345678", Password: "abc12345", SMSCode: tc.code})
				if !errors.Is(err, tc.want) || len(store.users) != 0 {
					t.Fatalf("required verification must reject registration without persisting a user: got %v, want %v", err, tc.want)
				}
			})
		}
	}
}

func TestMVPRegistrationPolicyDoesNotBypassSMSLogin(t *testing.T) {
	store := newFakeStore()
	svc := service.NewAuthService(store, commonauth.NewManager("secret"),
		service.WithRegistrationSMSRequired(false), service.WithSMS(service.NewMockSMSService()))
	_, err := svc.LoginBySMS(context.Background(), "13812345678", "not-a-code", "127.0.0.1")
	if !errors.Is(err, service.ErrSMSCodeInvalid) || len(store.users) != 0 {
		t.Fatalf("disabled registration verification must not affect SMS login: got %v", err)
	}
}

func authRequest(t *testing.T, router http.Handler, path string, payload map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}
