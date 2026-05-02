package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

const cookieName = "wb_session"

type contextKey int

const sessionKey contextKey = 0

// Session holds authenticated user identity stored in Redis with 24h TTL.
type Session struct {
	ForemanID   string `json:"foreman_id"`
	ForemanName string `json:"foreman_name"`
	Role        string `json:"role"` // "super_admin" | "foreman"
	City        string `json:"city,omitempty"`
	CityNorm    string `json:"city_norm,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
}

func sessionTokenKey(token string) string {
	return fmt.Sprintf("workbot:session:%s", token)
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (h *Handler) saveSession(r *http.Request, sess Session) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(sess)
	if err != nil {
		return "", err
	}
	err = h.store.rdb.Set(r.Context(), sessionTokenKey(token), data, 24*time.Hour).Err()
	return token, err
}

func (h *Handler) resolveSession(r *http.Request, token string) (*Session, error) {
	data, err := h.store.rdb.Get(r.Context(), sessionTokenKey(token)).Bytes()
	if err == goredis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (h *Handler) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := h.extractToken(r)
		if token == "" {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		sess, err := h.resolveSession(r, token)
		if err != nil || sess == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, sess)
		next(w, r.WithContext(ctx))
	})
}

func (h *Handler) superAdminOnly(next http.HandlerFunc) http.Handler {
	return h.auth(func(w http.ResponseWriter, r *http.Request) {
		sess := sessionFromCtx(r)
		if sess == nil || sess.Role != "super_admin" {
			http.Error(w, "Доступ запрещён", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func sessionFromCtx(r *http.Request) *Session {
	sess, _ := r.Context().Value(sessionKey).(*Session)
	return sess
}

func (h *Handler) extractToken(r *http.Request) string {
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}

func (h *Handler) showLogin(w http.ResponseWriter, r *http.Request) {
	flash := r.URL.Query().Get("flash")
	h.tmpl.Render(w, "login.html", map[string]any{"Flash": flash})
}

func (h *Handler) doLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	login := r.FormValue("login")
	password := r.FormValue("password")

	var sess Session

	if login == h.superAdminLogin && password == h.adminToken {
		sess = Session{ForemanID: "super_admin", ForemanName: "Администратор", Role: "super_admin"}
	} else {
		foreman, err := h.store.FindForemanByLogin(r.Context(), login)
		if err != nil || foreman == nil {
			http.Redirect(w, r, "/admin/login?flash=Неверный+логин+или+пароль", http.StatusSeeOther)
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(foreman.PassHash), []byte(password)); err != nil {
			http.Redirect(w, r, "/admin/login?flash=Неверный+логин+или+пароль", http.StatusSeeOther)
			return
		}
		cityNorm := strings.ToLower(strings.ReplaceAll(foreman.City, " ", "_"))
		sess = Session{ForemanID: foreman.ID, ForemanName: foreman.Name, Role: "foreman", City: foreman.City, CityNorm: cityNorm, Timezone: foreman.Timezone}
	}

	token, err := h.saveSession(r, sess)
	if err != nil {
		h.logger.Error("save session", "error", err)
		http.Redirect(w, r, "/admin/login?flash=Ошибка+сервера", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400,
	})
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (h *Handler) doLogout(w http.ResponseWriter, r *http.Request) {
	if token := h.extractToken(r); token != "" {
		h.store.rdb.Del(r.Context(), sessionTokenKey(token))
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/admin",
		MaxAge:   -1,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
